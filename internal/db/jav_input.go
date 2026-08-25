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
)

var ErrJavInputEmpty = errors.New("JAV input is empty")

var javInputCreateMu sync.Mutex

var leadingNumericJavInputCodePattern = regexp.MustCompile(`^\s*\d{4,}[-_]\d{2,}`)
var dateLikeJavInputTokenPattern = regexp.MustCompile(`^\s*\d{4}[-_/]\d{1,2}[-_/]\d{1,2}(?:\D|$)`)

type javInputLibraryMatch struct {
	ID   int64
	Code string
}

type javInputHistoryMatch struct {
	JavInputBatchID int64
	NormalizedCode  string
}

// CreateJavInputBatch persists the original input and both de-duplication stages atomically.
func CreateJavInputBatch(ctx context.Context, rawInput string) (*models.JavInputBatch, error) {
	items, batch := prepareJavInputBatch(rawInput)
	if batch.InputCount == 0 {
		return nil, ErrJavInputEmpty
	}
	if common.DB == nil {
		return nil, errors.New("database is not initialized")
	}

	// The partial unique index is the durable invariant; this process-level lock also
	// makes concurrent submissions deterministic instead of surfacing a uniqueness error.
	javInputCreateMu.Lock()
	defer javInputCreateMu.Unlock()

	err := common.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		uniqueCodes := make([]string, 0, batch.BatchUniqueCount)
		for i := range items {
			if items[i].Status == "" {
				uniqueCodes = append(uniqueCodes, items[i].NormalizedCode)
			}
		}

		libraryMatches, err := listJavInputLibraryMatches(tx, uniqueCodes)
		if err != nil {
			return err
		}
		historyMatches, err := listJavInputHistoryMatches(tx, uniqueCodes)
		if err != nil {
			return err
		}

		for i := range items {
			item := &items[i]
			if item.Status != "" {
				continue
			}
			if match, ok := libraryMatches[item.NormalizedCode]; ok {
				item.Status = models.JavInputStatusDuplicateLibrary
				item.ExistingJavID = &match.ID
				batch.LibraryDuplicateCount++
				continue
			}
			if match, ok := historyMatches[item.NormalizedCode]; ok {
				item.Status = models.JavInputStatusDuplicateHistory
				item.ExistingBatchID = &match.JavInputBatchID
				batch.HistoryDuplicateCount++
				continue
			}
			item.Status = models.JavInputStatusAccepted
			batch.AcceptedCount++
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
		return nil
	})
	if err != nil {
		return nil, err
	}
	batch.Items = items
	return &batch, nil
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
	var builder strings.Builder
	for _, char := range strings.ToUpper(strings.TrimSpace(value)) {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			builder.WriteRune(char)
		}
	}
	return builder.String()
}

func listJavInputLibraryMatches(tx *gorm.DB, normalizedCodes []string) (map[string]javInputLibraryMatch, error) {
	result := make(map[string]javInputLibraryMatch)
	if len(normalizedCodes) == 0 {
		return result, nil
	}
	var rows []javInputLibraryMatch
	if err := tx.
		Table("jav j").
		Select("DISTINCT j.id, j.code").
		Joins("JOIN video_location vl ON vl.jav_id = j.id").
		Joins("JOIN directory d ON d.id = vl.directory_id").
		Where(activeLocationWhereSQL("vl", "d")).
		Where("COALESCE(j.code, '') <> ''").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list final JAV codes for input de-duplication: %w", err)
	}
	wanted := make(map[string]struct{}, len(normalizedCodes))
	for _, code := range normalizedCodes {
		wanted[code] = struct{}{}
	}
	for _, row := range rows {
		key := normalizeJavInputCode(row.Code)
		if _, ok := wanted[key]; ok {
			result[key] = row
		}
	}
	return result, nil
}

func listJavInputHistoryMatches(tx *gorm.DB, normalizedCodes []string) (map[string]javInputHistoryMatch, error) {
	result := make(map[string]javInputHistoryMatch)
	if len(normalizedCodes) == 0 {
		return result, nil
	}
	var rows []javInputHistoryMatch
	if err := tx.
		Model(&models.JavInputItem{}).
		Select("jav_input_batch_id, normalized_code").
		Where("status = ?", models.JavInputStatusAccepted).
		Where("normalized_code IN ?", normalizedCodes).
		Order("id ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list historical JAV input codes: %w", err)
	}
	for _, row := range rows {
		if _, exists := result[row.NormalizedCode]; !exists {
			result[row.NormalizedCode] = row
		}
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

// DeleteJavInputBatch removes one complete input snapshot and releases any
// accepted-code reservations owned by it. Historical duplicates stay as
// snapshots, but no longer point at a batch that has been removed.
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

// DeleteAllJavInputBatches clears the raw-input workspace without touching the
// final JAV library or any real files.
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
		return nil
	})
}

// ClearJavInputPreprocessed globally empties the preprocessing pool while
// preserving batch history. Cleared codes no longer occupy the accepted-code
// unique index and can therefore be submitted again.
func ClearJavInputPreprocessed(ctx context.Context) (int64, error) {
	if common.DB == nil {
		return 0, errors.New("database is not initialized")
	}

	javInputCreateMu.Lock()
	defer javInputCreateMu.Unlock()

	result := common.DB.WithContext(ctx).
		Model(&models.JavInputItem{}).
		Where("status = ?", models.JavInputStatusAccepted).
		Update("status", models.JavInputStatusCleared)
	if result.Error != nil {
		return 0, fmt.Errorf("clear preprocessed JAV input items: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// ListJavInputPreprocessed returns the final output of both de-duplication
// stages. Accepted codes are hidden as soon as a matching final library work
// gains an active real-file location.
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

	query := common.DB.WithContext(ctx).
		Model(&models.JavInputItem{}).
		Where("status = ?", models.JavInputStatusAccepted)
	search = strings.TrimSpace(search)
	if search != "" {
		pattern := "%" + search + "%"
		query = query.Where("code LIKE ? COLLATE NOCASE", pattern)
	}

	var candidates []models.JavInputItem
	if err := query.Order("id DESC").Find(&candidates).Error; err != nil {
		return nil, 0, fmt.Errorf("list accepted JAV input items: %w", err)
	}
	normalizedCodes := make([]string, 0, len(candidates))
	for _, item := range candidates {
		normalizedCodes = append(normalizedCodes, item.NormalizedCode)
	}
	libraryMatches, err := listJavInputLibraryMatches(common.DB.WithContext(ctx), normalizedCodes)
	if err != nil {
		return nil, 0, err
	}

	filtered := make([]models.JavInputPreprocessedItem, 0, len(candidates))
	for _, item := range candidates {
		if _, exists := libraryMatches[item.NormalizedCode]; exists {
			continue
		}
		filtered = append(filtered, models.JavInputPreprocessedItem{
			ID:              item.ID,
			JavInputBatchID: item.JavInputBatchID,
			LineNumber:      item.LineNumber,
			RawLine:         item.RawLine,
			Code:            item.Code,
			NormalizedCode:  item.NormalizedCode,
			CreatedAt:       item.CreatedAt,
		})
	}

	total := int64(len(filtered))
	start := (page - 1) * pageSize
	if start >= len(filtered) {
		return []models.JavInputPreprocessedItem{}, total, nil
	}
	end := start + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[start:end], total, nil
}
