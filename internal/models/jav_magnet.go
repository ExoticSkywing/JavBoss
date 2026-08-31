package models

import "time"

const (
	JavMagnetReviewPending  = "pending"
	JavMagnetReviewAccepted = "accepted"
	JavMagnetReviewRejected = "rejected"

	JavDownloadBatchPending   = "pending"
	JavDownloadBatchSubmitted = "submitted"
	JavDownloadBatchPartial   = "partial"
	JavDownloadBatchFailed    = "failed"
	JavDownloadBatchCompleted = "completed"

	JavDownloadAttemptPending         = "pending"
	JavDownloadAttemptSubmitted       = "submitted"
	JavDownloadAttemptDownloaded      = "downloaded"
	JavDownloadAttemptAwaitingQuality = "awaiting_quality"
	JavDownloadAttemptAccepted        = "accepted"
	JavDownloadAttemptRejected        = "rejected"
	JavDownloadAttemptFailed          = "failed"
	JavDownloadAttemptUncertain       = "uncertain"
)

// JavMagnetCandidate stores one source magnet as a durable, idempotent fact.
// The info hash is the identity; source metadata can be refreshed, but manual
// review fields are intentionally kept separate and never overwritten.
type JavMagnetCandidate struct {
	ID              int64      `json:"id" gorm:"primaryKey"`
	JavID           int64      `json:"jav_id" gorm:"not null;index:idx_jav_magnet_candidate_jav_id;uniqueIndex:idx_jav_magnet_candidate_jav_hash,priority:1"`
	InfoHash        string     `json:"info_hash" gorm:"not null;uniqueIndex:idx_jav_magnet_candidate_jav_hash,priority:2"`
	URI             string     `json:"uri" gorm:"type:text;not null"`
	Name            string     `json:"name" gorm:"type:text;not null;default:''"`
	SizeMiB         int64      `json:"size_mib" gorm:"not null;default:0"`
	HD              bool       `json:"hd" gorm:"not null;default:0"`
	CNSub           bool       `json:"cnsub" gorm:"not null;default:0"`
	Files           int        `json:"files" gorm:"not null;default:0"`
	SourceCreatedAt string     `json:"source_created_at,omitempty" gorm:"type:text;not null;default:''"`
	FirstSeenAt     time.Time  `json:"first_seen_at" gorm:"not null"`
	LastSeenAt      time.Time  `json:"last_seen_at" gorm:"not null"`
	ReviewStatus    string     `json:"review_status" gorm:"not null;default:pending;index:idx_jav_magnet_candidate_review_status"`
	QualityClear    *bool      `json:"quality_clear,omitempty"`
	Confirmed1080P  *bool      `json:"confirmed_1080p,omitempty"`
	HasIntroAd      *bool      `json:"has_intro_ad,omitempty"`
	HasWatermark    *bool      `json:"has_watermark,omitempty"`
	HasMarquee      *bool      `json:"has_marquee,omitempty"`
	IsUncensored    *bool      `json:"is_uncensored,omitempty"`
	ReviewReasons   string     `json:"review_reasons,omitempty" gorm:"type:text;not null;default:''"`
	ReviewNotes     string     `json:"review_notes,omitempty" gorm:"type:text;not null;default:''"`
	ReviewedAt      *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// JavMagnetSelection is the one currently selected candidate for a work.
// Selecting a candidate means “ready for a later send”, never “best forever”.
type JavMagnetSelection struct {
	JavID       int64     `json:"jav_id" gorm:"primaryKey"`
	CandidateID int64     `json:"candidate_id" gorm:"not null;index:idx_jav_magnet_selection_candidate_id"`
	SelectedAt  time.Time `json:"selected_at" gorm:"not null"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"not null"`
}

// JavDownloadBatch groups one or more submissions. A single-work send is the
// same operation with a one-item batch, which keeps retry and audit semantics
// identical for both UI paths.
type JavDownloadBatch struct {
	ID              int64                `json:"id" gorm:"primaryKey"`
	Status          string               `json:"status" gorm:"not null;default:pending;index"`
	ExternalBatchID string               `json:"external_batch_id,omitempty" gorm:"type:text;not null;default:''"`
	Error           string               `json:"error,omitempty" gorm:"type:text;not null;default:''"`
	CreatedAt       time.Time            `json:"created_at"`
	SubmittedAt     *time.Time           `json:"submitted_at,omitempty"`
	UpdatedAt       time.Time            `json:"updated_at"`
	Attempts        []JavDownloadAttempt `json:"attempts,omitempty" gorm:"foreignKey:BatchID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
}

// JavDownloadAttempt is the durable hand-off record for one work/candidate.
// ExternalTaskID and IdempotencyKey let a future cloud service safely retry.
type JavDownloadAttempt struct {
	ID             int64      `json:"id" gorm:"primaryKey"`
	BatchID        int64      `json:"batch_id" gorm:"not null;index:idx_jav_download_attempt_batch_id"`
	JavID          int64      `json:"jav_id" gorm:"not null;index:idx_jav_download_attempt_jav_id"`
	CandidateID    int64      `json:"candidate_id" gorm:"not null;index"`
	IdempotencyKey string     `json:"idempotency_key" gorm:"not null;uniqueIndex:idx_jav_download_attempt_idempotency_key"`
	ExternalTaskID string     `json:"external_task_id,omitempty" gorm:"type:text;not null;default:''"`
	Status         string     `json:"status" gorm:"not null;default:pending;index"`
	Error          string     `json:"error,omitempty" gorm:"type:text;not null;default:''"`
	CreatedAt      time.Time  `json:"created_at"`
	SubmittedAt    *time.Time `json:"submitted_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

// JavQualityAcceptance records the irreversible business decision that makes
// a downloaded asset a real library import. One work has at most one current
// accepted record; rejected candidate history remains on its candidate row.
type JavQualityAcceptance struct {
	ID          int64     `json:"id" gorm:"primaryKey"`
	JavID       int64     `json:"jav_id" gorm:"not null;uniqueIndex:idx_jav_quality_acceptance_jav_id"`
	CandidateID int64     `json:"candidate_id" gorm:"not null;index"`
	AttemptID   *int64    `json:"attempt_id,omitempty" gorm:"index"`
	VideoID     *int64    `json:"video_id,omitempty" gorm:"index"`
	LocationID  *int64    `json:"location_id,omitempty" gorm:"index"`
	AcceptedAt  time.Time `json:"accepted_at" gorm:"not null;index"`
	Notes       string    `json:"notes,omitempty" gorm:"type:text;not null;default:''"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
