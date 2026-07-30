package models

import "time"

const (
	DownloaderProviderCloudDrive2 = "clouddrive2"
	DownloaderProviderOpenList    = "openlist"

	DiscoveryDownloadQueued             = "queued"
	DiscoveryDownloadOfflineDownloading = "offline_downloading"
	DiscoveryDownloadResolvingFiles     = "resolving_files"
	DiscoveryDownloadWaitingLocal       = "waiting_local_download"
	DiscoveryDownloadLocalDownloading   = "local_downloading"
	DiscoveryDownloadCompleted          = "completed"
	DiscoveryDownloadFailed             = "failed"
	DiscoveryDownloadCanceled           = "canceled"
)

// DownloaderSettings contains the provider-independent singleton settings.
type DownloaderSettings struct {
	ID               int64     `json:"-" gorm:"primaryKey"`
	ActiveProvider   string    `json:"active_provider" gorm:"not null;default:''"`
	DirectoryID      *int64    `json:"directory_id" gorm:"index"`
	LocalConcurrency int       `json:"local_concurrency" gorm:"not null;default:2"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// DownloaderProviderSettings stores the shared connection fields for one
// provider. Provider-specific validation belongs to the provider adapter.
type DownloaderProviderSettings struct {
	Provider     string    `json:"provider" gorm:"primaryKey"`
	Address      string    `json:"address" gorm:"not null;default:''"`
	APIToken     string    `json:"-" gorm:"type:text;not null;default:''"`
	RemoteFolder string    `json:"remote_folder" gorm:"not null;default:''"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// JavDiscoveryDownload is a persistent one-click download job. The remote
// URL is kept server-side because magnet and download URLs may contain tokens.
type JavDiscoveryDownload struct {
	ID                 int64            `json:"id" gorm:"primaryKey"`
	JavDiscoveryItemID int64            `json:"jav_discovery_item_id" gorm:"not null;index;uniqueIndex:idx_jav_discovery_download_target_hash,priority:1"`
	JavDiscoveryItem   JavDiscoveryItem `json:"-" gorm:"foreignKey:JavDiscoveryItemID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
	DirectoryID        int64            `json:"directory_id" gorm:"not null;index;uniqueIndex:idx_jav_discovery_download_target_hash,priority:2"`
	Directory          Directory        `json:"-" gorm:"foreignKey:DirectoryID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`
	Provider           string           `json:"provider" gorm:"not null"`
	InfoHash           string           `json:"info_hash" gorm:"not null;uniqueIndex:idx_jav_discovery_download_target_hash,priority:3"`
	MagnetURL          string           `json:"-" gorm:"type:text;not null"`
	MagnetName         string           `json:"magnet_name" gorm:"not null;default:''"`
	RemoteFolder       string           `json:"remote_folder" gorm:"not null;default:''"`
	RemoteTaskID       string           `json:"remote_task_id" gorm:"not null;default:''"`
	Status             string           `json:"status" gorm:"not null;default:queued;index"`
	BytesTotal         int64            `json:"bytes_total" gorm:"not null;default:0"`
	BytesDownloaded    int64            `json:"bytes_downloaded" gorm:"not null;default:0"`
	LocalFilesJSON     string           `json:"-" gorm:"type:text;not null;default:'[]'"`
	ErrorMessage       string           `json:"error_message" gorm:"type:text;not null;default:''"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
	CompletedAt        *time.Time       `json:"completed_at"`
}
