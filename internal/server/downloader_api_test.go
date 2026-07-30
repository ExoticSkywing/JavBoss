package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"javboss/internal/common"
	"javboss/internal/db"
	"javboss/internal/models"

	"github.com/gin-gonic/gin"
)

func TestDownloaderAPIKeepsTokenWriteOnlyAndQueuesForActiveProvider(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
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
	item := models.JavDiscoveryItem{
		Code:            "ABC-001",
		MetadataJSON:    `{}`,
		MagnetLinksJSON: `[{"name":"ABC-001 HD","url":"magnet:?xt=urn:btih:0123456789ABCDEF0123456789ABCDEF01234567&dn=ABC-001"}]`,
	}
	if err := database.Create(&item).Error; err != nil {
		t.Fatalf("create discovery item: %v", err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router)
	providerBody, _ := json.Marshal(map[string]any{
		"address": "http://127.0.0.1:5244", "api_token": "secret-token",
		"remote_folder": "/115/JavBoss",
	})
	saved := performDiscoveryRequest(t, router, http.MethodPut, "/jav/discovery/downloader/providers/openlist", providerBody)
	if saved.Code != http.StatusOK {
		t.Fatalf("save provider status = %d body=%s", saved.Code, saved.Body.String())
	}
	if value := saved.Body.String(); strings.Contains(value, "secret-token") {
		t.Fatalf("settings response exposed token: %s", value)
	}
	cloudDriveBody, _ := json.Marshal(map[string]any{
		"address": "http://127.0.0.1:19798", "api_token": "cloud-secret-token",
		"remote_folder": "/115/JavBoss",
	})
	saved = performDiscoveryRequest(t, router, http.MethodPut, "/jav/discovery/downloader/providers/clouddrive2", cloudDriveBody)
	if saved.Code != http.StatusOK || strings.Contains(saved.Body.String(), "cloud-secret-token") {
		t.Fatalf("save CloudDrive2 provider status=%d body=%s", saved.Code, saved.Body.String())
	}
	settingsBody, _ := json.Marshal(map[string]any{
		"active_provider": models.DownloaderProviderCloudDrive2,
		"directory_id":    directory.ID, "local_concurrency": 3,
	})
	saved = performDiscoveryRequest(t, router, http.MethodPut, "/jav/discovery/downloader/settings", settingsBody)
	if saved.Code != http.StatusOK {
		t.Fatalf("save settings status = %d body=%s", saved.Code, saved.Body.String())
	}
	if value := saved.Body.String(); !strings.Contains(value, `"local_concurrency":3`) {
		t.Fatalf("settings response omitted local concurrency: %s", value)
	}
	loaded := performDiscoveryRequest(t, router, http.MethodGet, "/jav/discovery/downloader/settings", nil)
	if loaded.Code != http.StatusOK || strings.Contains(loaded.Body.String(), "secret-token") {
		t.Fatalf("get settings status=%d body=%s", loaded.Code, loaded.Body.String())
	}

	downloadBody, _ := json.Marshal(map[string]any{
		"magnet_url": "magnet:?xt=urn:btih:0123456789ABCDEF0123456789ABCDEF01234567&dn=ABC-001",
	})
	queued := performDiscoveryRequest(t, router, http.MethodPost, "/jav/discovery/items/"+strconv.FormatInt(item.ID, 10)+"/downloads", downloadBody)
	if queued.Code != http.StatusCreated {
		t.Fatalf("queue download status = %d body=%s", queued.Code, queued.Body.String())
	}
	jobs, err := db.ListDiscoveryDownloads(context.Background(), 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("list jobs = %#v, %v", jobs, err)
	}
	if jobs[0].Status != models.DiscoveryDownloadQueued || jobs[0].Code != "ABC-001" || jobs[0].Provider != models.DownloaderProviderCloudDrive2 {
		t.Fatalf("unexpected queued job: %+v", jobs[0])
	}
}
