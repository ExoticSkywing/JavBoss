package db

import (
	"context"
	"path/filepath"
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
	detail, err := GetJavInputBatch(ctx, batch.ID)
	if err != nil {
		t.Fatalf("get batch: %v", err)
	}
	if len(detail.Items) != 6 || detail.Items[0].RawLine != batch.Items[0].RawLine {
		t.Fatalf("history detail did not preserve ordered original lines: %#v", detail.Items)
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
