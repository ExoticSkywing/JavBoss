package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"javboss/internal/common"
	"javboss/internal/models"

	"gorm.io/gorm"
)

// JavMagnetSample is the flat, scan-friendly representation used by the
// magnet sample library. It deliberately keeps the objective source fields
// beside the human review facts so samples can be compared without opening
// every work detail.
type JavMagnetSample struct {
	ID              int64      `json:"id" gorm:"column:id"`
	JavID           int64      `json:"jav_id" gorm:"column:jav_id"`
	Code            string     `json:"code" gorm:"column:code"`
	Title           string     `json:"title" gorm:"column:title"`
	Name            string     `json:"name" gorm:"column:name"`
	InfoHash        string     `json:"info_hash" gorm:"column:info_hash"`
	SizeMiB         int64      `json:"size_mib" gorm:"column:size_mi_b"`
	Files           int        `json:"files" gorm:"column:files"`
	HD              bool       `json:"hd" gorm:"column:hd"`
	CNSub           bool       `json:"cnsub" gorm:"column:cn_sub"`
	QualityClear    *bool      `json:"quality_clear,omitempty" gorm:"column:quality_clear"`
	Confirmed1080P  *bool      `json:"confirmed_1080p,omitempty" gorm:"column:confirmed1080_p"`
	HasIntroAd      *bool      `json:"has_intro_ad,omitempty" gorm:"column:has_intro_ad"`
	HasWatermark    *bool      `json:"has_watermark,omitempty" gorm:"column:has_watermark"`
	HasMarquee      *bool      `json:"has_marquee,omitempty" gorm:"column:has_marquee"`
	IsUncensored    *bool      `json:"is_uncensored,omitempty" gorm:"column:is_uncensored"`
	ReviewStatus    string     `json:"review_status" gorm:"column:review_status"`
	ReviewReasons   string     `json:"review_reasons,omitempty" gorm:"column:review_reasons"`
	ReviewNotes     string     `json:"review_notes,omitempty" gorm:"column:review_notes"`
	SourceCreatedAt string     `json:"source_created_at,omitempty" gorm:"column:source_created_at"`
	FirstSeenAt     time.Time  `json:"first_seen_at" gorm:"column:first_seen_at"`
	LastSeenAt      time.Time  `json:"last_seen_at" gorm:"column:last_seen_at"`
	ReviewedAt      *time.Time `json:"reviewed_at,omitempty" gorm:"column:reviewed_at"`
	AcceptedAt      *time.Time `json:"accepted_at,omitempty" gorm:"column:accepted_at"`
}

// JavMagnetSampleStats is calculated over the currently filtered sample set.
// Keeping counts next to the list makes the page useful for pattern finding
// without requiring the user to mentally total a paginated result.
type JavMagnetSampleStats struct {
	Total          int64 `json:"total"`
	Accepted       int64 `json:"accepted"`
	Rejected       int64 `json:"rejected"`
	HD             int64 `json:"hd"`
	CNSub          int64 `json:"cnsub"`
	Uncensored     int64 `json:"uncensored"`
	QualityClear   int64 `json:"quality_clear"`
	Confirmed1080P int64 `json:"confirmed_1080p"`
	PreferredSize  int64 `json:"preferred_size"`
	IntroAd        int64 `json:"intro_ad"`
	Watermark      int64 `json:"watermark"`
	Marquee        int64 `json:"marquee"`
}

func javMagnetSampleBase(ctx context.Context, status, search string) *gorm.DB {
	query := common.DB.WithContext(ctx).
		Table("jav_magnet_candidate mc").
		Joins("JOIN jav j ON j.id = mc.jav_id").
		Joins("LEFT JOIN jav_quality_acceptance qa ON qa.jav_id = mc.jav_id AND qa.candidate_id = mc.id").
		Where("mc.review_status IN ?", []string{models.JavMagnetReviewAccepted, models.JavMagnetReviewRejected}).
		Where("mc.review_status = ? OR qa.id IS NOT NULL", models.JavMagnetReviewRejected)

	switch strings.ToLower(strings.TrimSpace(status)) {
	case models.JavMagnetReviewAccepted, models.JavMagnetReviewRejected:
		query = query.Where("mc.review_status = ?", strings.ToLower(strings.TrimSpace(status)))
	}
	if term := strings.TrimSpace(search); term != "" {
		like := "%" + term + "%"
		query = query.Where("j.code LIKE ? OR j.title LIKE ? OR mc.name LIKE ? OR mc.info_hash LIKE ?", like, like, like, like)
	}
	return query
}

func javMagnetSampleSort(sort, direction string) string {
	column := map[string]string{
		"accepted_at": "COALESCE(qa.accepted_at, mc.reviewed_at, mc.updated_at)",
		"reviewed_at": "mc.reviewed_at",
		"size":        "mc.size_mi_b",
		"files":       "mc.files",
		"code":        "j.code",
	}[strings.ToLower(strings.TrimSpace(sort))]
	if column == "" {
		column = "COALESCE(qa.accepted_at, mc.reviewed_at, mc.updated_at)"
	}
	order := "DESC"
	if strings.EqualFold(strings.TrimSpace(direction), "asc") {
		order = "ASC"
	}
	return column + " " + order
}

// ListJavMagnetSamples lists reviewed candidates (accepted and rejected) with
// a compact aggregate. Pending candidates remain in the work detail because
// they are not yet useful training samples.
func ListJavMagnetSamples(ctx context.Context, status, search, sort, direction string, limit, offset int) ([]JavMagnetSample, int64, JavMagnetSampleStats, error) {
	if limit <= 0 {
		limit = 40
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	base := javMagnetSampleBase(ctx, status, search)
	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, JavMagnetSampleStats{}, fmt.Errorf("count JAV magnet samples: %w", err)
	}

	var aggregate struct {
		Total          int64 `gorm:"column:total"`
		Accepted       int64 `gorm:"column:accepted"`
		Rejected       int64 `gorm:"column:rejected"`
		HD             int64 `gorm:"column:hd"`
		CNSub          int64 `gorm:"column:cnsub"`
		Uncensored     int64 `gorm:"column:uncensored"`
		QualityClear   int64 `gorm:"column:quality_clear"`
		Confirmed1080P int64 `gorm:"column:confirmed_1080p"`
		PreferredSize  int64 `gorm:"column:preferred_size"`
		IntroAd        int64 `gorm:"column:intro_ad"`
		Watermark      int64 `gorm:"column:watermark"`
		Marquee        int64 `gorm:"column:marquee"`
	}
	if err := base.Session(&gorm.Session{}).Select(`
		COUNT(*) AS total,
		COALESCE(SUM(CASE WHEN mc.review_status = 'accepted' THEN 1 ELSE 0 END), 0) AS accepted,
		COALESCE(SUM(CASE WHEN mc.review_status = 'rejected' THEN 1 ELSE 0 END), 0) AS rejected,
		COALESCE(SUM(CASE WHEN mc.hd = 1 THEN 1 ELSE 0 END), 0) AS hd,
		COALESCE(SUM(CASE WHEN mc.cn_sub = 1 THEN 1 ELSE 0 END), 0) AS cnsub,
		COALESCE(SUM(CASE WHEN mc.is_uncensored = 1 THEN 1 ELSE 0 END), 0) AS uncensored,
		COALESCE(SUM(CASE WHEN mc.quality_clear = 1 THEN 1 ELSE 0 END), 0) AS quality_clear,
		COALESCE(SUM(CASE WHEN mc.confirmed1080_p = 1 THEN 1 ELSE 0 END), 0) AS confirmed_1080p,
		COALESCE(SUM(CASE WHEN mc.size_mi_b BETWEEN 5120 AND 10240 THEN 1 ELSE 0 END), 0) AS preferred_size,
		COALESCE(SUM(CASE WHEN mc.has_intro_ad = 1 THEN 1 ELSE 0 END), 0) AS intro_ad,
		COALESCE(SUM(CASE WHEN mc.has_watermark = 1 THEN 1 ELSE 0 END), 0) AS watermark,
		COALESCE(SUM(CASE WHEN mc.has_marquee = 1 THEN 1 ELSE 0 END), 0) AS marquee`).Scan(&aggregate).Error; err != nil {
		return nil, 0, JavMagnetSampleStats{}, fmt.Errorf("summarize JAV magnet samples: %w", err)
	}

	var samples []JavMagnetSample
	if err := base.Session(&gorm.Session{}).
		Select(`mc.id, mc.jav_id, j.code, j.title, mc.name, mc.info_hash, mc.size_mi_b, mc.files, mc.hd, mc.cn_sub,
			mc.quality_clear, mc.confirmed1080_p, mc.has_intro_ad, mc.has_watermark, mc.has_marquee, mc.is_uncensored,
			mc.review_status, mc.review_reasons, mc.review_notes, mc.source_created_at, mc.first_seen_at, mc.last_seen_at,
			mc.reviewed_at, qa.accepted_at`).
		Order(javMagnetSampleSort(sort, direction)).
		Order("mc.id DESC").
		Limit(limit).
		Offset(offset).
		Scan(&samples).Error; err != nil {
		return nil, 0, JavMagnetSampleStats{}, fmt.Errorf("list JAV magnet samples: %w", err)
	}
	if samples == nil {
		samples = []JavMagnetSample{}
	}
	return samples, total, JavMagnetSampleStats{
		Total: total, Accepted: aggregate.Accepted, Rejected: aggregate.Rejected,
		HD: aggregate.HD, CNSub: aggregate.CNSub, Uncensored: aggregate.Uncensored,
		QualityClear: aggregate.QualityClear, Confirmed1080P: aggregate.Confirmed1080P,
		PreferredSize: aggregate.PreferredSize, IntroAd: aggregate.IntroAd,
		Watermark: aggregate.Watermark, Marquee: aggregate.Marquee,
	}, nil
}
