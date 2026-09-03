package models

import "time"

// JavCodeAlias keeps a previous normalized code attached to the canonical
// work after a user corrects a typo. Aliases are durable identity facts, not
// input-history rows, so clearing old input batches cannot release a code for
// a second work.
type JavCodeAlias struct {
	ID             int64     `json:"id" gorm:"primaryKey"`
	JavID          int64     `json:"jav_id" gorm:"not null;index:idx_jav_code_alias_jav_id"`
	Jav            Jav       `json:"-" gorm:"foreignKey:JavID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
	NormalizedCode string    `json:"normalized_code" gorm:"not null;uniqueIndex:idx_jav_code_alias_normalized_code"`
	Code           string    `json:"code" gorm:"not null;default:''"`
	CreatedAt      time.Time `json:"created_at"`
}
