package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"javboss/internal/common"
	"javboss/internal/models"
)

func TestClaimNextQueuedDiscoveryDownloadClaimsEachJobOnce(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	previousDB := common.DB
	common.DB = database
	t.Cleanup(func() {
		common.DB = previousDB
		if sqlDB, dbErr := database.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	directory := models.Directory{Path: t.TempDir()}
	if err := database.Create(&directory).Error; err != nil {
		t.Fatalf("create directory: %v", err)
	}
	item := models.JavDiscoveryItem{Code: "ABC-001", MetadataJSON: `{}`, MagnetLinksJSON: `[]`}
	if err := database.Create(&item).Error; err != nil {
		t.Fatalf("create discovery item: %v", err)
	}
	if err := SaveDownloaderSettings(context.Background(), &models.DownloaderSettings{
		ActiveProvider: models.DownloaderProviderOpenList, LocalConcurrency: 2,
	}); err != nil {
		t.Fatalf("save downloader settings: %v", err)
	}
	for _, hash := range []string{"hash-one", "hash-two"} {
		job := models.JavDiscoveryDownload{
			JavDiscoveryItemID: item.ID, DirectoryID: directory.ID,
			InfoHash: hash, MagnetURL: "magnet:?xt=urn:btih:" + hash,
			Provider: models.DownloaderProviderOpenList,
		}
		if err := CreateDiscoveryDownload(context.Background(), &job); err != nil {
			t.Fatalf("create download %s: %v", hash, err)
		}
	}

	start := make(chan struct{})
	results := make(chan *models.JavDiscoveryDownload, 2)
	claimErrors := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			job, claimErr := ClaimNextQueuedDiscoveryDownload(context.Background(), models.DownloaderProviderOpenList)
			results <- job
			claimErrors <- claimErr
		}()
	}
	close(start)

	claimed := make(map[int64]bool, 2)
	for range 2 {
		if claimErr := <-claimErrors; claimErr != nil {
			t.Fatalf("claim download: %v", claimErr)
		}
		job := <-results
		if job == nil {
			t.Fatal("claim returned no job")
		}
		if claimed[job.ID] {
			t.Fatalf("job %d was claimed twice", job.ID)
		}
		if job.Status != models.DiscoveryDownloadOfflineDownloading {
			t.Fatalf("claimed status = %q", job.Status)
		}
		claimed[job.ID] = true
	}
	if err := SaveDownloaderSettings(context.Background(), &models.DownloaderSettings{
		ActiveProvider: models.DownloaderProviderCloudDrive2, LocalConcurrency: 2,
	}); !errors.Is(err, ErrDownloaderProviderHasActiveJobs) {
		t.Fatalf("switch provider with unfinished jobs error = %v", err)
	}
}
