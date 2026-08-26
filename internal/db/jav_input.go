package db

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"javboss/internal/common"
	"javboss/internal/models"
	"javboss/internal/util"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrJavInputEmpty = errors.New("JAV input is empty")
var ErrJavInputNoCodes = errors.New("JAV input contains no recognizable codes")

var javInputCreateMu sync.Mutex

var leadingNumericJavInputCodePattern = regexp.MustCompile(`^\s*\d{4,}[-_]\d{2,}`)
var dateLikeJavInputTokenPattern = regexp.MustCompile(`^\s*\d{4}[-_/]\d{1,2}[-_/]\d{1,2}(?:\D|$)`)

type javInputLibraryMatch struct {
	ID             int64
	Code           string
	NormalizedCode string `gorm:"column:normalized_code"`
}

// CreateJavInputBatch atomically merges parsed codes into the canonical JAV
// collection. Batch rows retain the original input only as an audit receipt.
func CreateJavInputBatch(ctx context.Context, rawInput string) (*models.JavInputBatch, error) {
	return createJavInputBatch(ctx, rawInput, nil)
}

func createJavInputBatch(ctx context.Context, rawInput string, afterSnapshot func(*gorm.DB) error) (*models.JavInputBatch, error) {
	_, validationBatch := prepareJavInputBatch(rawInput)
	if validationBatch.InputCount == 0 {
		return nil, ErrJavInputEmpty
	}
	if validationBatch.ParsedCount == 0 {
		return nil, ErrJavInputNoCodes
	}
	if common.DB == nil {
		return nil, errors.New("database is not initialized")
	}

	// The canonical JAV unique index is the durable invariant; this process-level
	// lock keeps batch outcome counts deterministic within one server process.
	javInputCreateMu.Lock()
	defer javInputCreateMu.Unlock()

	var committedItems []models.JavInputItem
	var committedBatch models.JavInputBatch
	err := withSQLiteTransactionRetry(ctx, common.DB, func(tx *gorm.DB) error {
		// A failed SQLite snapshot must not leak statuses, counters, or IDs into
		// the next attempt. Rebuild all mutable attempt state from the raw input.
		items, batch := prepareJavInputBatch(rawInput)
		committedItems = nil
		committedBatch = models.JavInputBatch{}

		uniqueCodes := make([]string, 0, batch.BatchUniqueCount)
		for i := range items {
			if items[i].Status == "" {
				uniqueCodes = append(uniqueCodes, items[i].NormalizedCode)
			}
		}

		works, err := listJavInputWorks(tx, uniqueCodes)
		if err != nil {
			return err
		}
		libraryMatches, err := listJavInputLibraryMatches(tx, uniqueCodes)
		if err != nil {
			return err
		}
		if afterSnapshot != nil {
			if err := afterSnapshot(tx); err != nil {
				return err
			}
		}

		for i := range items {
			item := &items[i]
			if item.Status != "" {
				continue
			}
			if match, ok := works[item.NormalizedCode]; ok {
				item.JavID = &match.ID
				createdAt := nowForJavInputItem(item)
				// A legacy imported work may predate jav_acquisition.  Every
				// duplicate input still points at the canonical lifecycle row so
				// later unlink/replacement operations can reconcile it correctly.
				if err := ensureJavAcquisitionTx(tx, match.ID, createdAt); err != nil {
					return err
				}
				if err := advanceJavAcquisitionAfterMetadataTx(tx, match.ID, createdAt); err != nil {
					return err
				}
				if err := reconcileJavAcquisitionStagesTx(tx, []int64{match.ID}); err != nil {
					return err
				}
				if libraryMatch, imported := libraryMatches[item.NormalizedCode]; imported {
					item.Status = models.JavInputStatusDuplicateLibrary
					item.ExistingJavID = &libraryMatch.ID
					batch.LibraryDuplicateCount++
					continue
				}
				item.Status = models.JavInputStatusDuplicateHistory
				if match.JavInputBatchID != nil {
					item.ExistingBatchID = match.JavInputBatchID
				}
				batch.HistoryDuplicateCount++
				continue
			}

			created, err := createCanonicalJavForInputTx(tx, item.Code, item.NormalizedCode, item.CreatedAt)
			if err != nil {
				return err
			}
			// A concurrent writer can win the normalized-code insert. Reclassify
			// deterministically instead of surfacing a uniqueness error.
			if !created.Created {
				item.JavID = &created.Jav.ID
				if err := ensureJavAcquisitionTx(tx, created.Jav.ID, item.CreatedAt); err != nil {
					return err
				}
				if err := advanceJavAcquisitionAfterMetadataTx(tx, created.Jav.ID, item.CreatedAt); err != nil {
					return err
				}
				if err := reconcileJavAcquisitionStagesTx(tx, []int64{created.Jav.ID}); err != nil {
					return err
				}
				item.Status = models.JavInputStatusDuplicateHistory
				batch.HistoryDuplicateCount++
				continue
			}
			item.JavID = &created.Jav.ID
			if err := ensureJavAcquisitionTx(tx, created.Jav.ID, item.CreatedAt); err != nil {
				return err
			}
			item.Status = models.JavInputStatusAccepted
			batch.AcceptedCount++
		}
		javIDByCode := make(map[string]int64, batch.BatchUniqueCount)
		for i := range items {
			if items[i].JavID != nil {
				javIDByCode[items[i].NormalizedCode] = *items[i].JavID
			}
		}
		for i := range items {
			if items[i].Status == models.JavInputStatusDuplicateBatch {
				javID, ok := javIDByCode[items[i].NormalizedCode]
				if !ok || javID <= 0 {
					return fmt.Errorf("resolve canonical JAV for batch duplicate %q", items[i].NormalizedCode)
				}
				items[i].JavID = &javID
			}
		}

		if err := tx.Create(&batch).Error; err != nil {
			return fmt.Errorf("create JAV input batch: %w", err)
		}
		for i := range items {
			items[i].JavInputBatchID = batch.ID
		}
		if err := tx.Create(&items).Error; err != nil {
			return fmt.Errorf("create JAV input items: %w", err)
		}
		committedBatch = batch
		committedItems = items
		return nil
	})
	if err != nil {
		return nil, err
	}
	committedBatch.Items = committedItems
	return &committedBatch, nil
}

type javInputWorkMatch struct {
	ID              int64  `gorm:"column:id"`
	NormalizedCode  string `gorm:"column:normalized_code"`
	JavInputBatchID *int64 `gorm:"column:jav_input_batch_id"`
}

type createCanonicalJavResult struct {
	Jav     models.Jav
	Created bool
}

func listJavInputWorks(tx *gorm.DB, normalizedCodes []string) (map[string]javInputWorkMatch, error) {
	result := make(map[string]javInputWorkMatch, len(normalizedCodes))
	if len(normalizedCodes) == 0 {
		return result, nil
	}
	var rows []javInputWorkMatch
	if err := tx.
		Table("jav j").
		Select(`j.id, j.normalized_code, (
			SELECT MIN(jii.jav_input_batch_id)
			FROM jav_input_item jii
			WHERE jii.jav_id = j.id
		) AS jav_input_batch_id`).
		Where("j.normalized_code IN ?", normalizedCodes).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list canonical JAV works for input: %w", err)
	}
	for _, row := range rows {
		result[row.NormalizedCode] = row
	}
	return result, nil
}

func createCanonicalJavForInputTx(tx *gorm.DB, code, normalizedCode string, createdAt time.Time) (*createCanonicalJavResult, error) {
	if tx == nil {
		return nil, errors.New("tx is nil")
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	normalizedCode = models.NormalizeJavCode(normalizedCode)
	if normalizedCode == "" {
		return nil, models.ErrInvalidJavCode
	}
	item := models.Jav{
		Code:           code,
		NormalizedCode: normalizedCode,
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
	}
	insert := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "normalized_code"}},
		DoNothing: true,
	}).Create(&item)
	if insert.Error != nil {
		return nil, fmt.Errorf("create canonical JAV work from input: %w", insert.Error)
	}
	if insert.RowsAffected > 0 {
		return &createCanonicalJavResult{Jav: item, Created: true}, nil
	}
	if err := tx.Where("normalized_code = ?", normalizedCode).First(&item).Error; err != nil {
		return nil, fmt.Errorf("load concurrently created canonical JAV work: %w", err)
	}
	return &createCanonicalJavResult{Jav: item}, nil
}

func ensureJavAcquisitionTx(tx *gorm.DB, javID int64, createdAt time.Time) error {
	if tx == nil {
		return errors.New("tx is nil")
	}
	if javID <= 0 {
		return errors.New("jav id must be positive")
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	item := models.JavAcquisition{
		JavID:     javID,
		Stage:     models.JavAcquisitionStageMetadataPending,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&item).Error; err != nil {
		return fmt.Errorf("ensure JAV acquisition: %w", err)
	}
	return nil
}

func nowForJavInputItem(item *models.JavInputItem) time.Time {
	if item == nil || item.CreatedAt.IsZero() {
		return time.Now().UTC()
	}
	return item.CreatedAt
}

func prepareJavInputBatch(rawInput string) ([]models.JavInputItem, models.JavInputBatch) {
	now := time.Now().UTC()
	batch := models.JavInputBatch{
		RawInput:  rawInput,
		CreatedAt: now,
		Preview:   javInputPreview(rawInput),
	}
	lines := strings.Split(strings.ReplaceAll(rawInput, "\r\n", "\n"), "\n")
	items := make([]models.JavInputItem, 0, len(lines))
	firstLineByCode := make(map[string]int)

	for index, value := range lines {
		rawLine := strings.TrimSuffix(value, "\r")
		if strings.TrimSpace(rawLine) == "" {
			continue
		}
		batch.InputCount++
		codes := extractJavInputCodes(rawLine)
		if len(codes) == 0 {
			status := models.JavInputStatusNote
			if containsDigit(rawLine) {
				status = models.JavInputStatusInvalid
				batch.InvalidCount++
			}
			items = append(items, models.JavInputItem{
				LineNumber: index + 1,
				RawLine:    rawLine,
				Status:     status,
				CreatedAt:  now,
			})
			continue
		}

		for _, code := range codes {
			item := models.JavInputItem{
				LineNumber: index + 1,
				RawLine:    rawLine,
				Code:       strings.ToUpper(strings.TrimSpace(code)),
				CreatedAt:  now,
			}
			item.NormalizedCode = normalizeJavInputCode(item.Code)
			if item.NormalizedCode == "" {
				continue
			}
			batch.ParsedCount++
			if firstLine, exists := firstLineByCode[item.NormalizedCode]; exists {
				item.Status = models.JavInputStatusDuplicateBatch
				item.DuplicateOfLine = firstLine
				batch.BatchDuplicateCount++
			} else {
				firstLineByCode[item.NormalizedCode] = item.LineNumber
				batch.BatchUniqueCount++
			}
			items = append(items, item)
		}
	}
	return items, batch
}

func extractJavInputCodes(rawLine string) []string {
	parts := strings.FieldsFunc(rawLine, func(char rune) bool {
		return unicode.IsSpace(char) || strings.ContainsRune(",，;；、|", char)
	})
	codes := make([]string, 0, len(parts))
	for _, part := range parts {
		if code := firstValidJavInputCode(part); code != "" {
			codes = append(codes, code)
		}
	}
	if len(codes) > 0 {
		return codes
	}
	if code := firstValidJavInputCode(rawLine); code != "" {
		return []string{code}
	}
	return nil
}

func firstValidJavInputCode(value string) string {
	if dateLikeJavInputTokenPattern.MatchString(value) {
		return ""
	}
	for _, code := range util.ExtractCodeFromName(value) {
		if containsASCIILetter(code) || leadingNumericJavInputCodePattern.MatchString(value) {
			return code
		}
	}
	return ""
}

func containsDigit(value string) bool {
	for _, char := range value {
		if unicode.IsDigit(char) {
			return true
		}
	}
	return false
}

func javInputPreview(rawInput string) string {
	for _, line := range strings.Split(strings.ReplaceAll(rawInput, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" {
			continue
		}
		runes := []rune(line)
		if len(runes) > 80 {
			runes = runes[:80]
		}
		return string(runes)
	}
	return ""
}

func containsASCIILetter(value string) bool {
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') {
			return true
		}
	}
	return false
}

func normalizeJavInputCode(value string) string {
	if codes := util.ExtractCodeFromName(value); len(codes) > 0 {
		value = codes[0]
	}
	return models.NormalizeJavCode(value)
}

func listJavInputLibraryMatches(tx *gorm.DB, normalizedCodes []string) (map[string]javInputLibraryMatch, error) {
	result := make(map[string]javInputLibraryMatch)
	if len(normalizedCodes) == 0 {
		return result, nil
	}
	var rows []javInputLibraryMatch
	if err := tx.
		Table("jav j").
		Select("DISTINCT j.id, j.code, j.normalized_code").
		Joins("JOIN video_location vl ON vl.jav_id = j.id").
		Joins("JOIN directory d ON d.id = vl.directory_id").
		Where(activeLocationWhereSQL("vl", "d")).
		Where("j.normalized_code IN ?", normalizedCodes).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list final JAV codes for input de-duplication: %w", err)
	}
	for _, row := range rows {
		result[row.NormalizedCode] = row
	}
	return result, nil
}

// ListJavInputBatches returns input history in reverse chronological order.
func ListJavInputBatches(ctx context.Context, page, pageSize int) ([]models.JavInputBatch, int64, error) {
	if common.DB == nil {
		return nil, 0, errors.New("database is not initialized")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	var total int64
	query := common.DB.WithContext(ctx).Model(&models.JavInputBatch{})
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count JAV input batches: %w", err)
	}
	var batches []models.JavInputBatch
	if err := query.
		Omit("raw_input").
		Order("id DESC").
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		Find(&batches).Error; err != nil {
		return nil, 0, fmt.Errorf("list JAV input batches: %w", err)
	}
	return batches, total, nil
}

// GetJavInputBatch returns one history record with its original ordered lines.
func GetJavInputBatch(ctx context.Context, id int64) (*models.JavInputBatch, error) {
	if common.DB == nil {
		return nil, errors.New("database is not initialized")
	}
	if id <= 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var batch models.JavInputBatch
	if err := common.DB.WithContext(ctx).
		Preload("Items", func(query *gorm.DB) *gorm.DB {
			return query.Order("line_number ASC, id ASC")
		}).
		First(&batch, id).Error; err != nil {
		return nil, err
	}
	return &batch, nil
}

// DeleteJavInputBatch removes one input audit snapshot. Canonical JAV works and
// their acquisition rows deliberately survive history deletion.
func DeleteJavInputBatch(ctx context.Context, id int64) error {
	if common.DB == nil {
		return errors.New("database is not initialized")
	}
	if id <= 0 {
		return gorm.ErrRecordNotFound
	}

	javInputCreateMu.Lock()
	defer javInputCreateMu.Unlock()

	return common.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&models.JavInputBatch{}).Where("id = ?", id).Count(&count).Error; err != nil {
			return fmt.Errorf("find JAV input batch for deletion: %w", err)
		}
		if count == 0 {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Model(&models.JavInputItem{}).
			Where("existing_batch_id = ?", id).
			Update("existing_batch_id", nil).Error; err != nil {
			return fmt.Errorf("clear deleted JAV input batch references: %w", err)
		}
		if err := tx.Delete(&models.JavInputBatch{}, id).Error; err != nil {
			return fmt.Errorf("delete JAV input batch: %w", err)
		}
		return nil
	})
}

// DeleteAllJavInputBatches clears raw-input audit history without touching the
// canonical JAV collection, acquisitions, or real files.
func DeleteAllJavInputBatches(ctx context.Context) error {
	if common.DB == nil {
		return errors.New("database is not initialized")
	}

	javInputCreateMu.Lock()
	defer javInputCreateMu.Unlock()

	return common.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&models.JavInputItem{}).Error; err != nil {
			return fmt.Errorf("delete all JAV input items: %w", err)
		}
		if err := tx.Where("1 = 1").Delete(&models.JavInputBatch{}).Error; err != nil {
			return fmt.Errorf("delete all JAV input batches: %w", err)
		}
		// A full workspace reset starts batch numbering from #1 again. Reset both
		// temporary input tables because no surviving row can reference their IDs.
		if err := tx.Exec(
			"DELETE FROM sqlite_sequence WHERE name IN (?, ?)",
			"jav_input_batch",
			"jav_input_item",
		).Error; err != nil {
			return fmt.Errorf("reset JAV input sequences: %w", err)
		}
		return nil
	})
}

// ClearJavInputPreprocessed is a compatibility no-op. Pending acquisitions are
// canonical works, not disposable workspace rows, so clearing them would
// violate the global collection invariant.
func ClearJavInputPreprocessed(ctx context.Context) (int64, error) {
	if common.DB == nil {
		return 0, errors.New("database is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return 0, nil
}

// ListJavInputPreprocessed returns acquisition works that do not yet have an
// active real-file location. Input history is optional provenance, not the
// source of truth, so deleting receipts does not remove works from this list.
func ListJavInputPreprocessed(ctx context.Context, page, pageSize int, search string) ([]models.JavInputPreprocessedItem, int64, error) {
	if common.DB == nil {
		return nil, 0, errors.New("database is not initialized")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	activeLocation := common.DB.WithContext(ctx).
		Table("video_location vl").
		Select("1").
		Joins("JOIN directory d ON d.id = vl.directory_id").
		Where("vl.jav_id = j.id").
		Where(activeLocationWhereSQL("vl", "d"))
	query := common.DB.WithContext(ctx).
		Table("jav_acquisition ja").
		Joins("JOIN jav j ON j.id = ja.jav_id").
		Where("NOT EXISTS (?)", activeLocation)
	search = strings.TrimSpace(search)
	if search != "" {
		pattern := "%" + search + "%"
		query = query.Where("j.code LIKE ? COLLATE NOCASE OR j.title LIKE ? COLLATE NOCASE", pattern, pattern)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count pending JAV acquisitions: %w", err)
	}
	var items []models.JavInputPreprocessedItem
	if err := query.
		Select(`j.id AS id,
			j.id AS jav_id,
			COALESCE((SELECT MIN(src.jav_input_batch_id) FROM jav_input_item src WHERE src.jav_id = j.id), 0) AS jav_input_batch_id,
			COALESCE((SELECT MIN(src.line_number) FROM jav_input_item src WHERE src.jav_id = j.id), 0) AS line_number,
			COALESCE((SELECT src.raw_line FROM jav_input_item src WHERE src.jav_id = j.id ORDER BY src.id LIMIT 1), '') AS raw_line,
			j.code,
			j.normalized_code,
			ja.stage,
			ja.created_at`).
		Order("ja.created_at DESC, ja.jav_id DESC").
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		Scan(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list pending JAV acquisitions: %w", err)
	}
	return items, total, nil
}
