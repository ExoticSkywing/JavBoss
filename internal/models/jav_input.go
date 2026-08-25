package models

import "time"

const (
	JavInputStatusAccepted         = "accepted"
	JavInputStatusDuplicateBatch   = "duplicate_batch"
	JavInputStatusDuplicateLibrary = "duplicate_library"
	JavInputStatusDuplicateHistory = "duplicate_history"
	JavInputStatusInvalid          = "invalid"
	JavInputStatusNote             = "note"
	JavInputStatusCleared          = "cleared"
)

// JavInputBatch is a complete snapshot of one single or bulk raw-code input.
// RawInput and each item's RawLine deliberately retain the user's original text.
type JavInputBatch struct {
	ID                    int64          `json:"id" gorm:"primaryKey"`
	RawInput              string         `json:"raw_input,omitempty" gorm:"type:text;not null"`
	InputCount            int            `json:"input_count" gorm:"not null;default:0"`
	ParsedCount           int            `json:"parsed_count" gorm:"not null;default:0"`
	BatchUniqueCount      int            `json:"batch_unique_count" gorm:"not null;default:0"`
	BatchDuplicateCount   int            `json:"batch_duplicate_count" gorm:"not null;default:0"`
	LibraryDuplicateCount int            `json:"library_duplicate_count" gorm:"not null;default:0"`
	HistoryDuplicateCount int            `json:"history_duplicate_count" gorm:"not null;default:0"`
	AcceptedCount         int            `json:"accepted_count" gorm:"not null;default:0"`
	InvalidCount          int            `json:"invalid_count" gorm:"not null;default:0"`
	CreatedAt             time.Time      `json:"created_at" gorm:"not null"`
	Preview               string         `json:"preview" gorm:"type:text;not null;default:''"`
	Items                 []JavInputItem `json:"items,omitempty" gorm:"foreignKey:JavInputBatchID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
}

// JavInputItem records the extracted code and the outcome of both de-duplication stages.
type JavInputItem struct {
	ID              int64     `json:"id" gorm:"primaryKey"`
	JavInputBatchID int64     `json:"batch_id" gorm:"not null;index:idx_jav_input_item_batch_id"`
	LineNumber      int       `json:"line_number" gorm:"not null"`
	RawLine         string    `json:"raw_line" gorm:"type:text;not null"`
	Code            string    `json:"code" gorm:"not null;default:''"`
	NormalizedCode  string    `json:"normalized_code" gorm:"not null;default:'';index:idx_jav_input_item_normalized_code;uniqueIndex:idx_jav_input_item_accepted_code,where:status = 'accepted'"`
	Status          string    `json:"status" gorm:"not null;index:idx_jav_input_item_status"`
	DuplicateOfLine int       `json:"duplicate_of_line" gorm:"not null;default:0"`
	ExistingBatchID *int64    `json:"existing_batch_id,omitempty" gorm:"index:idx_jav_input_item_existing_batch_id"`
	ExistingJavID   *int64    `json:"existing_jav_id,omitempty" gorm:"index:idx_jav_input_item_existing_jav_id"`
	CreatedAt       time.Time `json:"created_at" gorm:"not null"`
}

// JavInputPreprocessedItem is one globally accepted raw code that has not yet
// appeared as a final library work backed by an active real file.
type JavInputPreprocessedItem struct {
	ID              int64     `json:"id"`
	JavInputBatchID int64     `json:"batch_id"`
	LineNumber      int       `json:"line_number"`
	RawLine         string    `json:"raw_line"`
	Code            string    `json:"code"`
	NormalizedCode  string    `json:"normalized_code"`
	CreatedAt       time.Time `json:"created_at"`
}
