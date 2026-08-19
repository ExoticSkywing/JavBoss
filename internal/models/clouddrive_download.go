package models

import "time"

const (
	CloudDriveDownloadQueued             = "queued"
	CloudDriveDownloadOfflineDownloading = "offline_downloading"
	CloudDriveDownloadResolvingFiles     = "resolving_files"
	CloudDriveDownloadWaitingLocal       = "waiting_local_download"
	CloudDriveDownloadLocalDownloading   = "local_downloading"
	CloudDriveDownloadCompleted          = "completed"
	CloudDriveDownloadFailed             = "failed"
	CloudDriveDownloadCanceled           = "canceled"
)

// CloudDriveSettings is a single-row configuration for the CloudDrive2
// integration. APIToken is write-only at the HTTP API boundary.
type CloudDriveSettings struct {
	ID               int64     `json:"-" gorm:"primaryKey"`
	Address          string    `json:"address" gorm:"not null;default:''"`
	APIToken         string    `json:"-" gorm:"type:text;not null;default:''"`
	RemoteFolder     string    `json:"remote_folder" gorm:"not null;default:''"`
	DirectoryID      *int64    `json:"directory_id" gorm:"index"`
	Enabled          bool      `json:"enabled" gorm:"not null;default:0"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	LocalConcurrency int       `json:"local_concurrency" gorm:"not null;default:2"`
}

// JavDiscoveryDownload is a persistent one-click download job. The remote
// URL is kept server-side because magnet and download URLs may contain tokens.
type JavDiscoveryDownload struct {
	ID                 int64            `json:"id" gorm:"primaryKey"`
	JavDiscoveryItemID int64            `json:"jav_discovery_item_id" gorm:"not null;index;uniqueIndex:idx_jav_discovery_download_target_hash,priority:1"`
	JavDiscoveryItem   JavDiscoveryItem `json:"-" gorm:"foreignKey:JavDiscoveryItemID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
	DirectoryID        int64            `json:"directory_id" gorm:"not null;index;uniqueIndex:idx_jav_discovery_download_target_hash,priority:2"`
	Directory          Directory        `json:"-" gorm:"foreignKey:DirectoryID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`
	InfoHash           string           `json:"info_hash" gorm:"not null;uniqueIndex:idx_jav_discovery_download_target_hash,priority:3"`
	MagnetURL          string           `json:"-" gorm:"type:text;not null"`
	MagnetName         string           `json:"magnet_name" gorm:"not null;default:''"`
	RemoteFolder       string           `json:"remote_folder" gorm:"not null;default:''"`
	Status             string           `json:"status" gorm:"not null;default:queued;index"`
	Progress           float64          `json:"progress" gorm:"not null;default:0"`
	BytesTotal         int64            `json:"bytes_total" gorm:"not null;default:0"`
	BytesDownloaded    int64            `json:"bytes_downloaded" gorm:"not null;default:0"`
	LocalFilesJSON     string           `json:"-" gorm:"type:text;not null;default:'[]'"`
	ErrorMessage       string           `json:"error_message" gorm:"type:text;not null;default:''"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
	CompletedAt        *time.Time       `json:"completed_at"`
}
