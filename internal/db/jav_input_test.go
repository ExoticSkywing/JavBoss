package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"javboss/internal/common"
	"javboss/internal/models"

	"gorm.io/gorm"
)

func TestCreateJavInputBatchPreservesOriginalLinesAndExplainsBothDedupStages(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "jav-input.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	previousDB := common.DB
	common.DB = database
	t.Cleanup(func() {
		common.DB = previousDB
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})
	ctx := context.Background()

	historical, err := CreateJavInputBatch(ctx, "OLD-001 首次收集")
	if err != nil {
		t.Fatalf("create historical batch: %v", err)
	}
	if historical.AcceptedCount != 1 {
		t.Fatalf("historical accepted count = %d, want 1", historical.AcceptedCount)
	}

	libraryJavID := seedFinalJavForInputTest(t, database, "LIB-001")
	if err := database.Create(&models.Jav{Code: "ORPHAN-001", Title: "metadata without a real file"}).Error; err != nil {
		t.Fatalf("create orphan JAV metadata: %v", err)
	}

	raw := "  NEW-001 中文备注 原样保留  \r\nnew_001 第二次出现\r\nOLD-001 昨天已经输入\r\nLIB-001 已经入库\r\nORPHAN-001 只有元数据不算最终作品\r\n这行只有中文说明，记录于 2026-08-25"
	batch, err := CreateJavInputBatch(ctx, raw)
	if err != nil {
		t.Fatalf("create input batch: %v", err)
	}

	if batch.RawInput != raw {
		t.Fatalf("raw input changed:\n got: %q\nwant: %q", batch.RawInput, raw)
	}
	if batch.Preview != "NEW-001 中文备注 原样保留" {
		t.Fatalf("preview = %q, want first non-empty input line", batch.Preview)
	}
	if batch.InputCount != 6 || batch.ParsedCount != 5 || batch.BatchUniqueCount != 4 {
		t.Fatalf("unexpected input counts: %#v", batch)
	}
	if batch.BatchDuplicateCount != 1 || batch.LibraryDuplicateCount != 1 || batch.HistoryDuplicateCount != 1 {
		t.Fatalf("unexpected duplicate counts: %#v", batch)
	}
	if batch.AcceptedCount != 2 || batch.InvalidCount != 1 {
		t.Fatalf("unexpected final counts: %#v", batch)
	}
	if len(batch.Items) != 6 {
		t.Fatalf("item count = %d, want 6", len(batch.Items))
	}

	assertJavInputItem(t, batch.Items[0], 1, "  NEW-001 中文备注 原样保留  ", "NEW-001", models.JavInputStatusAccepted)
	assertJavInputItem(t, batch.Items[1], 2, "new_001 第二次出现", "NEW-001", models.JavInputStatusDuplicateBatch)
	if batch.Items[1].DuplicateOfLine != 1 {
		t.Fatalf("batch duplicate line = %d, want 1", batch.Items[1].DuplicateOfLine)
	}
	assertJavInputItem(t, batch.Items[2], 3, "OLD-001 昨天已经输入", "OLD-001", models.JavInputStatusDuplicateHistory)
	if batch.Items[2].ExistingBatchID == nil || *batch.Items[2].ExistingBatchID != historical.ID {
		t.Fatalf("historical duplicate batch = %v, want %d", batch.Items[2].ExistingBatchID, historical.ID)
	}
	assertJavInputItem(t, batch.Items[3], 4, "LIB-001 已经入库", "LIB-001", models.JavInputStatusDuplicateLibrary)
	if batch.Items[3].ExistingJavID == nil || *batch.Items[3].ExistingJavID != libraryJavID {
		t.Fatalf("library duplicate JAV = %v, want %d", batch.Items[3].ExistingJavID, libraryJavID)
	}
	assertJavInputItem(t, batch.Items[4], 5, "ORPHAN-001 只有元数据不算最终作品", "ORPHAN-001", models.JavInputStatusAccepted)
	assertJavInputItem(t, batch.Items[5], 6, "这行只有中文说明，记录于 2026-08-25", "", models.JavInputStatusInvalid)

	repeated, err := CreateJavInputBatch(ctx, "NEW-001 又输入了一次")
	if err != nil {
		t.Fatalf("create repeated batch: %v", err)
	}
	if repeated.AcceptedCount != 0 || repeated.HistoryDuplicateCount != 1 {
		t.Fatalf("repeated batch counts: %#v", repeated)
	}
	if repeated.Items[0].ExistingBatchID == nil || *repeated.Items[0].ExistingBatchID != batch.ID {
		t.Fatalf("repeated code points to batch %v, want %d", repeated.Items[0].ExistingBatchID, batch.ID)
	}

	batches, total, err := ListJavInputBatches(ctx, 1, 20)
	if err != nil {
		t.Fatalf("list batches: %v", err)
	}
	if total != 3 || len(batches) != 3 || batches[0].ID != repeated.ID {
		t.Fatalf("history order/total: total=%d batches=%#v", total, batches)
	}
	if batches[0].RawInput != "" {
		t.Fatalf("history summary unexpectedly loaded raw input: %q", batches[0].RawInput)
	}
	if batches[0].Preview != "NEW-001 又输入了一次" {
		t.Fatalf("history preview = %q", batches[0].Preview)
	}
	detail, err := GetJavInputBatch(ctx, batch.ID)
	if err != nil {
		t.Fatalf("get batch: %v", err)
	}
	if len(detail.Items) != 6 || detail.Items[0].RawLine != batch.Items[0].RawLine {
		t.Fatalf("history detail did not preserve ordered original lines: %#v", detail.Items)
	}
}

func TestDeleteJavInputBatchesReleasesAcceptedCodes(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "jav-input-delete.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	previousDB := common.DB
	common.DB = database
	t.Cleanup(func() {
		common.DB = previousDB
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})
	ctx := context.Background()

	accepted, err := CreateJavInputBatch(ctx, "DEL-001 first")
	if err != nil {
		t.Fatalf("create accepted batch: %v", err)
	}
	duplicate, err := CreateJavInputBatch(ctx, "DEL-001 second")
	if err != nil {
		t.Fatalf("create duplicate batch: %v", err)
	}
	if duplicate.Items[0].ExistingBatchID == nil || *duplicate.Items[0].ExistingBatchID != accepted.ID {
		t.Fatalf("duplicate does not point to original batch: %#v", duplicate.Items[0])
	}

	if err := DeleteJavInputBatch(ctx, accepted.ID); err != nil {
		t.Fatalf("delete accepted batch: %v", err)
	}
	if _, err := GetJavInputBatch(ctx, accepted.ID); err != gorm.ErrRecordNotFound {
		t.Fatalf("get deleted batch error = %v, want record not found", err)
	}
	duplicateDetail, err := GetJavInputBatch(ctx, duplicate.ID)
	if err != nil {
		t.Fatalf("get duplicate batch: %v", err)
	}
	if duplicateDetail.Items[0].ExistingBatchID != nil {
		t.Fatalf("deleted batch reference was retained: %#v", duplicateDetail.Items[0])
	}

	reaccepted, err := CreateJavInputBatch(ctx, "DEL-001 third")
	if err != nil {
		t.Fatalf("reaccept released code: %v", err)
	}
	if reaccepted.AcceptedCount != 1 {
		t.Fatalf("reaccepted count = %d, want 1", reaccepted.AcceptedCount)
	}

	if err := DeleteAllJavInputBatches(ctx); err != nil {
		t.Fatalf("delete all batches: %v", err)
	}
	batches, total, err := ListJavInputBatches(ctx, 1, 20)
	if err != nil {
		t.Fatalf("list cleared batches: %v", err)
	}
	if total != 0 || len(batches) != 0 {
		t.Fatalf("history was not cleared: total=%d batches=%#v", total, batches)
	}
	items, total, err := ListJavInputPreprocessed(ctx, 1, 20, "")
	if err != nil {
		t.Fatalf("list cleared preprocessed items: %v", err)
	}
	if total != 0 || len(items) != 0 {
		t.Fatalf("preprocessed works were not cleared: total=%d items=%#v", total, items)
	}
}

func TestListJavInputPreprocessedExcludesCodesWithActiveRealFiles(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "jav-input-preprocessed.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	previousDB := common.DB
	common.DB = database
	t.Cleanup(func() {
		common.DB = previousDB
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})
	ctx := context.Background()

	batch, err := CreateJavInputBatch(ctx, "PRE-001 first note\nPRE-002 searchable note")
	if err != nil {
		t.Fatalf("create preprocessed batch: %v", err)
	}
	items, total, err := ListJavInputPreprocessed(ctx, 1, 20, "")
	if err != nil {
		t.Fatalf("list preprocessed items: %v", err)
	}
	if total != 2 || len(items) != 2 || items[0].Code != "PRE-002" || items[1].Code != "PRE-001" {
		t.Fatalf("unexpected preprocessed items: total=%d items=%#v", total, items)
	}
	if items[0].JavInputBatchID != batch.ID || items[0].RawLine != "PRE-002 searchable note" {
		t.Fatalf("preprocessed item did not retain source: %#v", items[0])
	}

	seedFinalJavForInputTest(t, database, "pre_001")
	items, total, err = ListJavInputPreprocessed(ctx, 1, 20, "")
	if err != nil {
		t.Fatalf("list after final file appeared: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Code != "PRE-002" {
		t.Fatalf("final library code was not excluded: total=%d items=%#v", total, items)
	}

	items, total, err = ListJavInputPreprocessed(ctx, 1, 20, "PRE-002")
	if err != nil {
		t.Fatalf("search preprocessed items: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Code != "PRE-002" {
		t.Fatalf("search did not match raw line: total=%d items=%#v", total, items)
	}
}

func TestClearJavInputPreprocessedPreservesHistoryAndReleasesCodes(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "jav-input-clear-preprocessed.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	previousDB := common.DB
	common.DB = database
	t.Cleanup(func() {
		common.DB = previousDB
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})
	ctx := context.Background()

	batch, err := CreateJavInputBatch(ctx, "CLEAR-001\nCLEAR-002")
	if err != nil {
		t.Fatalf("create preprocessed batch: %v", err)
	}
	cleared, err := ClearJavInputPreprocessed(ctx)
	if err != nil {
		t.Fatalf("clear preprocessed works: %v", err)
	}
	if cleared != 2 {
		t.Fatalf("cleared count = %d, want 2", cleared)
	}
	items, total, err := ListJavInputPreprocessed(ctx, 1, 20, "")
	if err != nil {
		t.Fatalf("list cleared preprocessed works: %v", err)
	}
	if total != 0 || len(items) != 0 {
		t.Fatalf("preprocessed pool was not cleared: total=%d items=%#v", total, items)
	}
	detail, err := GetJavInputBatch(ctx, batch.ID)
	if err != nil {
		t.Fatalf("load preserved batch history: %v", err)
	}
	if len(detail.Items) != 2 || detail.Items[0].Status != models.JavInputStatusCleared || detail.Items[1].Status != models.JavInputStatusCleared {
		t.Fatalf("cleared history was not preserved: %#v", detail.Items)
	}

	reaccepted, err := CreateJavInputBatch(ctx, "CLEAR-001 再次输入")
	if err != nil {
		t.Fatalf("submit released code again: %v", err)
	}
	if reaccepted.AcceptedCount != 1 || reaccepted.HistoryDuplicateCount != 0 {
		t.Fatalf("released code was not accepted again: %#v", reaccepted)
	}
}

func TestCreateJavInputBatchSupportsGroupNotesAndMultipleCodesOnOneLine(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "jav-input-multiple-codes.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	previousDB := common.DB
	common.DB = database
	t.Cleanup(func() {
		common.DB = previousDB
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})

	raw := "极度美感\nVRTM-138 CORE-018 EBOD-502 BEB-095 JUFD-366 110316-005 REAL-475 SDMT-769 STARS-416 SVDVD-506 N1355 JUFD-366 DDOB-029 MIRD-150"
	batch, err := CreateJavInputBatch(context.Background(), raw)
	if err != nil {
		t.Fatalf("create multi-code batch: %v", err)
	}
	if batch.Preview != "极度美感" {
		t.Fatalf("preview = %q, want group title", batch.Preview)
	}
	if batch.InputCount != 2 || batch.ParsedCount != 14 || batch.BatchUniqueCount != 13 {
		t.Fatalf("unexpected multi-code counts: %#v", batch)
	}
	if batch.BatchDuplicateCount != 1 || batch.AcceptedCount != 13 || batch.InvalidCount != 0 {
		t.Fatalf("unexpected multi-code results: %#v", batch)
	}
	if len(batch.Items) != 15 {
		t.Fatalf("item count = %d, want one note plus fourteen code occurrences", len(batch.Items))
	}
	if batch.Items[0].Status != models.JavInputStatusNote || batch.Items[0].RawLine != "极度美感" {
		t.Fatalf("group title was not retained as a note: %#v", batch.Items[0])
	}
	wantCodes := []string{
		"VRTM-138", "CORE-018", "EBOD-502", "BEB-095", "JUFD-366", "110316-005", "REAL-475",
		"SDMT-769", "STARS-416", "SVDVD-506", "N1355", "JUFD-366", "DDOB-029", "MIRD-150",
	}
	for index, wantCode := range wantCodes {
		item := batch.Items[index+1]
		if item.Code != wantCode || item.LineNumber != 2 || item.RawLine != strings.Split(raw, "\n")[1] {
			t.Fatalf("item %d = %#v, want code %q from line 2", index+1, item, wantCode)
		}
	}
	duplicate := batch.Items[12]
	if duplicate.Status != models.JavInputStatusDuplicateBatch || duplicate.DuplicateOfLine != 2 {
		t.Fatalf("same-line JUFD duplicate = %#v", duplicate)
	}
}

func assertJavInputItem(t *testing.T, item models.JavInputItem, line int, rawLine, code, status string) {
	t.Helper()
	if item.LineNumber != line || item.RawLine != rawLine || item.Code != code || item.Status != status {
		t.Fatalf("item = %#v, want line=%d raw=%q code=%q status=%q", item, line, rawLine, code, status)
	}
}

func seedFinalJavForInputTest(t *testing.T, database *gorm.DB, code string) int64 {
	t.Helper()
	directory := models.Directory{Path: "/media/jav-input-final"}
	if err := database.Create(&directory).Error; err != nil {
		t.Fatalf("create directory: %v", err)
	}
	javRecord := models.Jav{Code: code, Title: "final work"}
	if err := database.Create(&javRecord).Error; err != nil {
		t.Fatalf("create JAV: %v", err)
	}
	video := models.Video{Fingerprint: "jav-input-final-video"}
	if err := database.Create(&video).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}
	location := models.VideoLocation{
		VideoID:      video.ID,
		DirectoryID:  directory.ID,
		RelativePath: code + ".mp4",
		Filename:     code + ".mp4",
		JavID:        &javRecord.ID,
	}
	if err := database.Create(&location).Error; err != nil {
		t.Fatalf("create video location: %v", err)
	}
	return javRecord.ID
}
