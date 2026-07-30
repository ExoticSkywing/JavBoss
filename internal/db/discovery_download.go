package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"javboss/internal/common"
	"javboss/internal/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrDiscoveryDownloadExists         = errors.New("discovery download already exists")
	ErrDownloaderProviderChanged       = errors.New("active downloader provider changed")
	ErrDownloaderProviderHasActiveJobs = errors.New("downloader provider has active jobs")
)

type DiscoveryDownloadResult struct {
	models.JavDiscoveryDownload
	Code          string   `json:"code"`
	DirectoryPath string   `json:"directory_path"`
	LocalFiles    []string `json:"local_files"`
}

func GetDownloaderSettings(ctx context.Context) (*models.DownloaderSettings, error) {
	if common.DB == nil {
		return nil, errors.New("get downloader settings: nil db")
	}
	var settings models.DownloaderSettings
	err := common.DB.WithContext(ctx).First(&settings, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &models.DownloaderSettings{ID: 1, LocalConcurrency: 2}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get downloader settings: %w", err)
	}
	return &settings, nil
}

func SaveDownloaderSettings(ctx context.Context, settings *models.DownloaderSettings) error {
	if common.DB == nil {
		return errors.New("save downloader settings: nil db")
	}
	if settings == nil {
		return errors.New("save downloader settings: missing settings")
	}
	settings.ID = 1
	settings.ActiveProvider = strings.TrimSpace(settings.ActiveProvider)
	if settings.LocalConcurrency < 1 || settings.LocalConcurrency > 5 {
		settings.LocalConcurrency = 2
	}
	if err := common.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current models.DownloaderSettings
		err := tx.First(&current, 1).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if current.ActiveProvider != settings.ActiveProvider {
			terminal := []string{
				models.DiscoveryDownloadCompleted,
				models.DiscoveryDownloadFailed,
				models.DiscoveryDownloadCanceled,
			}
			var count int64
			if err := tx.Model(&models.JavDiscoveryDownload{}).Where("status NOT IN ?", terminal).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return ErrDownloaderProviderHasActiveJobs
			}
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"active_provider", "directory_id", "local_concurrency", "updated_at",
			}),
		}).Create(settings).Error
	}); err != nil {
		if errors.Is(err, ErrDownloaderProviderHasActiveJobs) {
			return err
		}
		return fmt.Errorf("save downloader settings: %w", err)
	}
	return nil
}

func GetDownloaderProviderSettings(ctx context.Context, provider string) (*models.DownloaderProviderSettings, error) {
	if common.DB == nil {
		return nil, errors.New("get downloader provider settings: nil db")
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return nil, errors.New("get downloader provider settings: missing provider")
	}
	var settings models.DownloaderProviderSettings
	err := common.DB.WithContext(ctx).First(&settings, "provider = ?", provider).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &models.DownloaderProviderSettings{Provider: provider}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get downloader provider settings: %w", err)
	}
	return &settings, nil
}

func SaveDownloaderProviderSettings(ctx context.Context, settings *models.DownloaderProviderSettings) error {
	if common.DB == nil {
		return errors.New("save downloader provider settings: nil db")
	}
	if settings == nil {
		return errors.New("save downloader provider settings: missing settings")
	}
	settings.Provider = strings.TrimSpace(settings.Provider)
	if settings.Provider == "" {
		return errors.New("save downloader provider settings: missing provider")
	}
	settings.Address = strings.TrimSpace(settings.Address)
	settings.RemoteFolder = strings.TrimSpace(settings.RemoteFolder)
	if err := common.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "provider"}},
		DoUpdates: clause.AssignmentColumns([]string{"address", "api_token", "remote_folder", "updated_at"}),
	}).Create(settings).Error; err != nil {
		return fmt.Errorf("save downloader provider settings: %w", err)
	}
	return nil
}

func CreateDiscoveryDownload(ctx context.Context, job *models.JavDiscoveryDownload) error {
	if common.DB == nil {
		return errors.New("create discovery download: nil db")
	}
	if job == nil || job.JavDiscoveryItemID <= 0 || job.DirectoryID <= 0 || strings.TrimSpace(job.InfoHash) == "" {
		return errors.New("create discovery download: invalid job")
	}
	if job.Provider != models.DownloaderProviderCloudDrive2 && job.Provider != models.DownloaderProviderOpenList {
		return errors.New("create discovery download: invalid provider")
	}
	job.Status = models.DiscoveryDownloadQueued
	job.LocalFilesJSON = "[]"
	err := common.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var settings models.DownloaderSettings
		if err := tx.First(&settings, 1).Error; err != nil {
			return err
		}
		if settings.ActiveProvider != job.Provider {
			return ErrDownloaderProviderChanged
		}
		return tx.Create(job).Error
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return ErrDiscoveryDownloadExists
		}
		if errors.Is(err, ErrDownloaderProviderChanged) {
			return err
		}
		return fmt.Errorf("create discovery download: %w", err)
	}
	return nil
}

func GetDiscoveryDownload(ctx context.Context, id int64) (*models.JavDiscoveryDownload, error) {
	if common.DB == nil {
		return nil, errors.New("get discovery download: nil db")
	}
	var job models.JavDiscoveryDownload
	if err := common.DB.WithContext(ctx).First(&job, id).Error; err != nil {
		return nil, fmt.Errorf("get discovery download: %w", err)
	}
	return &job, nil
}

func ListDiscoveryDownloads(ctx context.Context, limit int) ([]DiscoveryDownloadResult, error) {
	if common.DB == nil {
		return nil, errors.New("list discovery downloads: nil db")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows []struct {
		models.JavDiscoveryDownload
		Code          string `gorm:"column:code"`
		DirectoryPath string `gorm:"column:directory_path"`
	}
	if err := common.DB.WithContext(ctx).
		Table("jav_discovery_download AS download").
		Select("download.*, item.code AS code, directory.path AS directory_path").
		Joins("JOIN jav_discovery_item AS item ON item.id = download.jav_discovery_item_id").
		Joins("JOIN directory ON directory.id = download.directory_id").
		Order("download.created_at DESC, download.id DESC").
		Limit(limit).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list discovery downloads: %w", err)
	}
	result := make([]DiscoveryDownloadResult, 0, len(rows))
	for _, row := range rows {
		files := []string{}
		_ = json.Unmarshal([]byte(row.LocalFilesJSON), &files)
		result = append(result, DiscoveryDownloadResult{
			JavDiscoveryDownload: row.JavDiscoveryDownload,
			Code:                 row.Code,
			DirectoryPath:        row.DirectoryPath,
			LocalFiles:           files,
		})
	}
	return result, nil
}

func ResetInterruptedDiscoveryDownloads(ctx context.Context) error {
	if common.DB == nil {
		return errors.New("reset discovery downloads: nil db")
	}
	active := []string{
		models.DiscoveryDownloadOfflineDownloading,
		models.DiscoveryDownloadResolvingFiles,
		models.DiscoveryDownloadWaitingLocal,
		models.DiscoveryDownloadLocalDownloading,
	}
	if err := common.DB.WithContext(ctx).Model(&models.JavDiscoveryDownload{}).
		Where("status IN ?", active).
		Updates(map[string]any{"status": models.DiscoveryDownloadQueued, "error_message": ""}).Error; err != nil {
		return fmt.Errorf("reset discovery downloads: %w", err)
	}
	return nil
}

func ClaimNextQueuedDiscoveryDownload(ctx context.Context, provider string) (*models.JavDiscoveryDownload, error) {
	if common.DB == nil {
		return nil, errors.New("claim discovery download: nil db")
	}
	if provider != models.DownloaderProviderCloudDrive2 && provider != models.DownloaderProviderOpenList {
		return nil, errors.New("claim discovery download: invalid provider")
	}
	for attempts := 0; attempts < 10; attempts++ {
		var job models.JavDiscoveryDownload
		err := common.DB.WithContext(ctx).
			Where("status = ? AND provider = ?", models.DiscoveryDownloadQueued, provider).
			Order("created_at, id").
			First(&job).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("claim discovery download: %w", err)
		}
		result := common.DB.WithContext(ctx).Model(&models.JavDiscoveryDownload{}).
			Where("id = ? AND status = ? AND provider = ?", job.ID, models.DiscoveryDownloadQueued, provider).
			Updates(map[string]any{
				"status": models.DiscoveryDownloadOfflineDownloading, "error_message": "",
			})
		if result.Error != nil {
			return nil, fmt.Errorf("claim discovery download: %w", result.Error)
		}
		if result.RowsAffected == 1 {
			job.Status = models.DiscoveryDownloadOfflineDownloading
			job.ErrorMessage = ""
			return &job, nil
		}
	}
	return nil, errors.New("claim discovery download: too much contention")
}

func UpdateDiscoveryDownload(ctx context.Context, id int64, updates map[string]any) error {
	if common.DB == nil {
		return errors.New("update discovery download: nil db")
	}
	if id <= 0 || len(updates) == 0 {
		return nil
	}
	if err := common.DB.WithContext(ctx).Model(&models.JavDiscoveryDownload{}).
		Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("update discovery download: %w", err)
	}
	return nil
}

func RetryDiscoveryDownload(ctx context.Context, id int64) error {
	result := common.DB.WithContext(ctx).Model(&models.JavDiscoveryDownload{}).
		Where("id = ? AND status IN ?", id, []string{models.DiscoveryDownloadFailed, models.DiscoveryDownloadCanceled}).
		Updates(map[string]any{
			"status": models.DiscoveryDownloadQueued, "error_message": "", "completed_at": nil,
			"remote_task_id": "", "bytes_total": 0, "bytes_downloaded": 0,
		})
	if result.Error != nil {
		return fmt.Errorf("retry discovery download: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func CancelDiscoveryDownload(ctx context.Context, id int64) error {
	result := common.DB.WithContext(ctx).Model(&models.JavDiscoveryDownload{}).
		Where("id = ? AND status NOT IN ?", id, []string{models.DiscoveryDownloadCompleted, models.DiscoveryDownloadCanceled}).
		Updates(map[string]any{"status": models.DiscoveryDownloadCanceled, "error_message": ""})
	if result.Error != nil {
		return fmt.Errorf("cancel discovery download: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func DeleteDiscoveryDownload(ctx context.Context, id int64) error {
	result := common.DB.WithContext(ctx).
		Where("id = ? AND status IN ?", id, []string{
			models.DiscoveryDownloadCompleted, models.DiscoveryDownloadFailed, models.DiscoveryDownloadCanceled,
		}).Delete(&models.JavDiscoveryDownload{})
	if result.Error != nil {
		return fmt.Errorf("delete discovery download: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func CompleteDiscoveryDownload(ctx context.Context, id int64, files []string, total int64) error {
	raw, err := json.Marshal(files)
	if err != nil {
		return fmt.Errorf("encode discovery download local files: %w", err)
	}
	now := time.Now().UTC()
	return UpdateDiscoveryDownload(ctx, id, map[string]any{
		"status":           models.DiscoveryDownloadCompleted,
		"bytes_total":      total,
		"bytes_downloaded": total,
		"local_files_json": string(raw),
		"error_message":    "",
		"completed_at":     &now,
	})
}
