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

var ErrCloudDriveDownloadExists = errors.New("cloud drive download already exists")

type CloudDriveDownloadResult struct {
	models.JavDiscoveryDownload
	Code          string   `json:"code"`
	DirectoryPath string   `json:"directory_path"`
	LocalFiles    []string `json:"local_files"`
}

func GetCloudDriveSettings(ctx context.Context) (*models.CloudDriveSettings, error) {
	if common.DB == nil {
		return nil, errors.New("get cloud drive settings: nil db")
	}
	var settings models.CloudDriveSettings
	err := common.DB.WithContext(ctx).First(&settings, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &models.CloudDriveSettings{ID: 1, LocalConcurrency: 2}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get cloud drive settings: %w", err)
	}
	return &settings, nil
}

func SaveCloudDriveSettings(ctx context.Context, settings *models.CloudDriveSettings) error {
	if common.DB == nil {
		return errors.New("save cloud drive settings: nil db")
	}
	if settings == nil {
		return errors.New("save cloud drive settings: missing settings")
	}
	settings.ID = 1
	settings.Address = strings.TrimSpace(settings.Address)
	settings.RemoteFolder = strings.TrimSpace(settings.RemoteFolder)
	if settings.LocalConcurrency < 1 || settings.LocalConcurrency > 5 {
		settings.LocalConcurrency = 2
	}
	if err := common.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"address", "api_token", "remote_folder", "directory_id", "local_concurrency", "enabled", "updated_at"}),
	}).Create(settings).Error; err != nil {
		return fmt.Errorf("save cloud drive settings: %w", err)
	}
	return nil
}

func CreateCloudDriveDownload(ctx context.Context, job *models.JavDiscoveryDownload) error {
	if common.DB == nil {
		return errors.New("create cloud drive download: nil db")
	}
	if job == nil || job.JavDiscoveryItemID <= 0 || job.DirectoryID <= 0 || strings.TrimSpace(job.InfoHash) == "" {
		return errors.New("create cloud drive download: invalid job")
	}
	job.Status = models.CloudDriveDownloadQueued
	job.Progress = 0
	job.LocalFilesJSON = "[]"
	result := common.DB.WithContext(ctx).Create(job)
	if result.Error != nil {
		if strings.Contains(strings.ToLower(result.Error.Error()), "unique") {
			return ErrCloudDriveDownloadExists
		}
		return fmt.Errorf("create cloud drive download: %w", result.Error)
	}
	return nil
}

func GetCloudDriveDownload(ctx context.Context, id int64) (*models.JavDiscoveryDownload, error) {
	if common.DB == nil {
		return nil, errors.New("get cloud drive download: nil db")
	}
	var job models.JavDiscoveryDownload
	if err := common.DB.WithContext(ctx).First(&job, id).Error; err != nil {
		return nil, fmt.Errorf("get cloud drive download: %w", err)
	}
	return &job, nil
}

func ListCloudDriveDownloads(ctx context.Context, limit int) ([]CloudDriveDownloadResult, error) {
	if common.DB == nil {
		return nil, errors.New("list cloud drive downloads: nil db")
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
		return nil, fmt.Errorf("list cloud drive downloads: %w", err)
	}
	result := make([]CloudDriveDownloadResult, 0, len(rows))
	for _, row := range rows {
		files := []string{}
		_ = json.Unmarshal([]byte(row.LocalFilesJSON), &files)
		result = append(result, CloudDriveDownloadResult{
			JavDiscoveryDownload: row.JavDiscoveryDownload,
			Code:                 row.Code,
			DirectoryPath:        row.DirectoryPath,
			LocalFiles:           files,
		})
	}
	return result, nil
}

func ResetInterruptedCloudDriveDownloads(ctx context.Context) error {
	if common.DB == nil {
		return errors.New("reset cloud drive downloads: nil db")
	}
	active := []string{
		models.CloudDriveDownloadOfflineDownloading,
		models.CloudDriveDownloadResolvingFiles,
		models.CloudDriveDownloadWaitingLocal,
		models.CloudDriveDownloadLocalDownloading,
	}
	if err := common.DB.WithContext(ctx).Model(&models.JavDiscoveryDownload{}).
		Where("status IN ?", active).
		Updates(map[string]any{"status": models.CloudDriveDownloadQueued, "error_message": ""}).Error; err != nil {
		return fmt.Errorf("reset cloud drive downloads: %w", err)
	}
	return nil
}

func ClaimNextQueuedCloudDriveDownload(ctx context.Context) (*models.JavDiscoveryDownload, error) {
	if common.DB == nil {
		return nil, errors.New("claim cloud drive download: nil db")
	}
	for attempts := 0; attempts < 10; attempts++ {
		var job models.JavDiscoveryDownload
		err := common.DB.WithContext(ctx).
			Where("status = ?", models.CloudDriveDownloadQueued).
			Order("created_at, id").
			First(&job).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("claim cloud drive download: %w", err)
		}
		result := common.DB.WithContext(ctx).Model(&models.JavDiscoveryDownload{}).
			Where("id = ? AND status = ?", job.ID, models.CloudDriveDownloadQueued).
			Updates(map[string]any{
				"status":   models.CloudDriveDownloadOfflineDownloading,
				"progress": 0, "error_message": "",
			})
		if result.Error != nil {
			return nil, fmt.Errorf("claim cloud drive download: %w", result.Error)
		}
		if result.RowsAffected == 1 {
			job.Status = models.CloudDriveDownloadOfflineDownloading
			job.Progress = 0
			job.ErrorMessage = ""
			return &job, nil
		}
	}
	return nil, errors.New("claim cloud drive download: too much contention")
}

func UpdateCloudDriveDownload(ctx context.Context, id int64, updates map[string]any) error {
	if common.DB == nil {
		return errors.New("update cloud drive download: nil db")
	}
	if id <= 0 || len(updates) == 0 {
		return nil
	}
	if err := common.DB.WithContext(ctx).Model(&models.JavDiscoveryDownload{}).
		Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("update cloud drive download: %w", err)
	}
	return nil
}

func RetryCloudDriveDownload(ctx context.Context, id int64) error {
	result := common.DB.WithContext(ctx).Model(&models.JavDiscoveryDownload{}).
		Where("id = ? AND status IN ?", id, []string{models.CloudDriveDownloadFailed, models.CloudDriveDownloadCanceled}).
		Updates(map[string]any{
			"status": models.CloudDriveDownloadQueued, "error_message": "", "completed_at": nil,
		})
	if result.Error != nil {
		return fmt.Errorf("retry cloud drive download: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func CancelCloudDriveDownload(ctx context.Context, id int64) error {
	result := common.DB.WithContext(ctx).Model(&models.JavDiscoveryDownload{}).
		Where("id = ? AND status NOT IN ?", id, []string{models.CloudDriveDownloadCompleted, models.CloudDriveDownloadCanceled}).
		Updates(map[string]any{"status": models.CloudDriveDownloadCanceled, "error_message": ""})
	if result.Error != nil {
		return fmt.Errorf("cancel cloud drive download: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func DeleteCloudDriveDownload(ctx context.Context, id int64) error {
	result := common.DB.WithContext(ctx).
		Where("id = ? AND status IN ?", id, []string{
			models.CloudDriveDownloadCompleted, models.CloudDriveDownloadFailed, models.CloudDriveDownloadCanceled,
		}).Delete(&models.JavDiscoveryDownload{})
	if result.Error != nil {
		return fmt.Errorf("delete cloud drive download: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func CompleteCloudDriveDownload(ctx context.Context, id int64, files []string, total int64) error {
	raw, err := json.Marshal(files)
	if err != nil {
		return fmt.Errorf("encode cloud drive local files: %w", err)
	}
	now := time.Now().UTC()
	return UpdateCloudDriveDownload(ctx, id, map[string]any{
		"status":           models.CloudDriveDownloadCompleted,
		"progress":         100,
		"bytes_total":      total,
		"bytes_downloaded": total,
		"local_files_json": string(raw),
		"error_message":    "",
		"completed_at":     &now,
	})
}
