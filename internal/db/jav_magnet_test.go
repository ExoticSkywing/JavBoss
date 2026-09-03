package db

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"javboss/internal/jav"
	"javboss/internal/models"

	"gorm.io/gorm"
)

func TestJavMagnetCandidatesAreIdempotentAndSelectionQueuesOnce(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	work := models.Jav{Code: "MAG-001", NormalizedCode: "MAG001", Title: "Magnet work"}
	if err := database.Create(&work).Error; err != nil {
		t.Fatalf("create JAV: %v", err)
	}
	magnets := []jav.JavDBAppMagnet{
		{Hash: "ABCDEF", Name: "MAG-001 1080p.mkv", Size: 8192, HD: true, CNSub: true, Files: 1},
		{Hash: "abcdef", Name: "duplicate hash", Size: 1024, Files: 1},
	}
	first, err := UpsertJavMagnetCandidates(ctx, work.ID, magnets)
	if err != nil {
		t.Fatalf("upsert magnets: %v", err)
	}
	if len(first) != 1 || first[0].InfoHash != "abcdef" || first[0].URI == "" {
		t.Fatalf("candidates = %#v", first)
	}
	second, err := UpsertJavMagnetCandidates(ctx, work.ID, magnets[:1])
	if err != nil {
		t.Fatalf("upsert magnets second time: %v", err)
	}
	if len(second) != 1 {
		t.Fatalf("candidate count after repeat = %d", len(second))
	}
	selection, err := SelectJavMagnet(ctx, work.ID, second[0].ID)
	if err != nil {
		t.Fatalf("select magnet: %v", err)
	}
	if selection.CandidateID != second[0].ID {
		t.Fatalf("selection = %#v", selection)
	}
	queue, total, err := ListJavDownloadQueue(ctx, 20, 0)
	if err != nil || total != 1 || len(queue) != 1 {
		t.Fatalf("queue total=%d items=%#v err=%v", total, queue, err)
	}
	batch, err := CreateJavDownloadBatch(ctx, []int64{work.ID})
	if err != nil {
		t.Fatalf("create download batch: %v", err)
	}
	if len(batch.Attempts) != 1 || batch.Attempts[0].Status != models.JavDownloadAttemptPending {
		t.Fatalf("batch = %#v", batch)
	}
	queue, total, err = ListJavDownloadQueue(ctx, 20, 0)
	if err != nil || total != 0 || len(queue) != 0 {
		t.Fatalf("queue after submit total=%d items=%#v err=%v", total, queue, err)
	}
	if _, err := CreateJavDownloadBatch(ctx, []int64{work.ID}); err == nil {
		t.Fatal("re-submitting active download should fail")
	}
}

func TestQualityApprovalWaitsForFormalLibraryScan(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	work := models.Jav{Code: "STAGED-001", NormalizedCode: "STAGED001", Title: "Staged work"}
	if err := database.Create(&work).Error; err != nil {
		t.Fatalf("create JAV: %v", err)
	}
	candidates, err := UpsertJavMagnetCandidates(ctx, work.ID, []jav.JavDBAppMagnet{{Hash: "staged-hash", Name: "STAGED-001"}})
	if err != nil {
		t.Fatalf("upsert magnet: %v", err)
	}
	if _, err := SelectJavMagnet(ctx, work.ID, candidates[0].ID); err != nil {
		t.Fatalf("select magnet: %v", err)
	}
	batch, err := CreateJavDownloadBatch(ctx, []int64{work.ID})
	if err != nil {
		t.Fatalf("create download batch: %v", err)
	}
	attemptID := batch.Attempts[0].ID
	stagingPath := "/115/云下载/jav待验收/STAGED-001"
	if _, err := MarkJavDownloadAttemptWithResultPaths(ctx, attemptID, models.JavDownloadAttemptAwaitingQuality, "staged-task", "", []string{stagingPath}); err != nil {
		t.Fatalf("mark awaiting quality: %v", err)
	}
	queue, total, err := ListJavQualityReviewQueue(ctx, 10, 0, nil)
	if err != nil || total != 1 || len(queue) != 1 {
		t.Fatalf("quality queue total=%d items=%#v err=%v", total, queue, err)
	}
	clear := true
	if _, err := SaveJavQualityReviewDecision(ctx, work.ID, candidates[0].ID, attemptID, JavMagnetReviewInput{Accepted: true, QualityClear: &clear}); err != nil {
		t.Fatalf("save quality review decision: %v", err)
	}
	queue, total, err = ListJavQualityReviewQueue(ctx, 10, 0, nil)
	if err != nil || total != 1 || len(queue) != 1 || queue[0].QualityReview == nil || queue[0].QualityReview.Decision != models.JavQualityReviewDecisionAccepted {
		t.Fatalf("quality queue decision total=%d items=%#v err=%v", total, queue, err)
	}
	submissions, err := ListJavQualityReviewSubmissions(ctx, []int64{attemptID})
	if err != nil || len(submissions) != 1 || submissions[0].Decision != models.JavQualityReviewDecisionAccepted {
		t.Fatalf("quality review submissions=%#v err=%v", submissions, err)
	}

	formalPath := "/115/正式作品库/STAGED-001"
	approved, err := ApproveJavDownloadedWorkWithReview(ctx, work.ID, candidates[0].ID, attemptID, []string{formalPath}, JavMagnetReviewInput{Accepted: true, Notes: "人工确认清晰"})
	if err != nil {
		t.Fatalf("approve staged download: %v", err)
	}
	if approved.Status != models.JavDownloadAttemptAwaitingScan || len(approved.ResultPaths) != 1 || approved.ResultPaths[0] != formalPath {
		t.Fatalf("approved attempt=%#v", approved)
	}
	var acceptanceCount int64
	if err := database.Model(&models.JavQualityAcceptance{}).Where("jav_id = ?", work.ID).Count(&acceptanceCount).Error; err != nil {
		t.Fatalf("count pre-scan acceptances: %v", err)
	}
	if acceptanceCount != 0 {
		t.Fatalf("formal acceptance existed before scan: %d", acceptanceCount)
	}

	directory := models.Directory{Path: "/formal-library"}
	video := models.Video{Fingerprint: "staged-video", DurationSec: 3600}
	if err := database.Create(&directory).Error; err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := database.Create(&video).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}
	location, err := UpsertVideoLocation(ctx, video.ID, directory.ID, "STAGED-001.mp4", time.Now().UTC())
	if err != nil {
		t.Fatalf("create scanned location: %v", err)
	}
	if err := SetVideoLocationJavIDForVideo(ctx, location.ID, video.ID, work.ID, location.UpdatedAt); err != nil {
		t.Fatalf("link scanned location: %v", err)
	}
	var acceptance models.JavQualityAcceptance
	if err := database.Where("jav_id = ?", work.ID).First(&acceptance).Error; err != nil {
		t.Fatalf("load finalized acceptance: %v", err)
	}
	if acceptance.AttemptID == nil || *acceptance.AttemptID != attemptID || acceptance.LocationID == nil || *acceptance.LocationID != location.ID {
		t.Fatalf("finalized acceptance=%#v", acceptance)
	}
	var acquisition models.JavAcquisition
	if err := database.First(&acquisition, work.ID).Error; err != nil {
		t.Fatalf("load finalized acquisition: %v", err)
	}
	if acquisition.Stage != models.JavAcquisitionStageImported {
		t.Fatalf("finalized stage=%q", acquisition.Stage)
	}
}

func TestJavMagnetCollectionRejectsStaleCode(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	work := models.Jav{Code: "STALE-001", NormalizedCode: "STALE001"}
	if err := database.Create(&work).Error; err != nil {
		t.Fatalf("create JAV: %v", err)
	}
	if err := database.Model(&models.Jav{}).Where("id = ?", work.ID).Updates(map[string]any{
		"code": "STALE-002", "normalized_code": "STALE002",
	}).Error; err != nil {
		t.Fatalf("change identity: %v", err)
	}
	if _, err := UpsertJavMagnetCandidatesForCode(ctx, work.ID, "STALE-001", []jav.JavDBAppMagnet{{Hash: "old-hash"}}); !errors.Is(err, ErrJavIdentityChanged) {
		t.Fatalf("stale magnet error = %v", err)
	}
	var count int64
	if err := database.Model(&models.JavMagnetCandidate{}).Where("jav_id = ?", work.ID).Count(&count).Error; err != nil {
		t.Fatalf("count stale candidates: %v", err)
	}
	if count != 0 {
		t.Fatalf("stale candidate count = %d", count)
	}
}

func TestJavDownloadBatchStatusIsDerivedFromAttempts(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	works := []models.Jav{
		{Code: "MAG-BATCH-001", NormalizedCode: "MAGBATCH001"},
		{Code: "MAG-BATCH-002", NormalizedCode: "MAGBATCH002"},
	}
	if err := database.Create(&works).Error; err != nil {
		t.Fatalf("create JAV works: %v", err)
	}
	for index := range works {
		candidates, err := UpsertJavMagnetCandidates(ctx, works[index].ID, []jav.JavDBAppMagnet{{Hash: works[index].Code, Name: works[index].Code}})
		if err != nil {
			t.Fatalf("upsert magnet %d: %v", index, err)
		}
		if _, err := SelectJavMagnet(ctx, works[index].ID, candidates[0].ID); err != nil {
			t.Fatalf("select magnet %d: %v", index, err)
		}
	}
	batch, err := CreateJavDownloadBatch(ctx, []int64{works[0].ID, works[1].ID})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if _, err := MarkJavDownloadAttempt(ctx, batch.Attempts[0].ID, models.JavDownloadAttemptSubmitted, "task-1", ""); err != nil {
		t.Fatalf("submit first attempt: %v", err)
	}
	partial, err := GetJavDownloadBatch(ctx, batch.ID)
	if err != nil || partial.Status != models.JavDownloadBatchPartial {
		t.Fatalf("partially submitted batch=%#v err=%v", partial, err)
	}
	if _, err := MarkJavDownloadAttempt(ctx, batch.Attempts[1].ID, models.JavDownloadAttemptSubmitted, "task-2", ""); err != nil {
		t.Fatalf("submit second attempt: %v", err)
	}
	submitted, err := GetJavDownloadBatch(ctx, batch.ID)
	if err != nil || submitted.Status != models.JavDownloadBatchSubmitted || submitted.SubmittedAt == nil {
		t.Fatalf("submitted batch=%#v err=%v", submitted, err)
	}
	for _, attempt := range batch.Attempts {
		if _, err := MarkJavDownloadAttempt(ctx, attempt.ID, models.JavDownloadAttemptAccepted, "", ""); err != nil {
			t.Fatalf("accept attempt: %v", err)
		}
	}
	completed, err := GetJavDownloadBatch(ctx, batch.ID)
	if err != nil || completed.Status != models.JavDownloadBatchCompleted {
		t.Fatalf("completed batch=%#v err=%v", completed, err)
	}
}

func TestJavDownloadAttemptIgnoresOutOfOrderAndTerminalCallbacks(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	work := models.Jav{Code: "MAG-ORDER-001", NormalizedCode: "MAGORDER001"}
	if err := database.Create(&work).Error; err != nil {
		t.Fatalf("create JAV: %v", err)
	}
	candidates, err := UpsertJavMagnetCandidates(ctx, work.ID, []jav.JavDBAppMagnet{{Hash: "order-hash"}})
	if err != nil {
		t.Fatalf("upsert candidate: %v", err)
	}
	if _, err := SelectJavMagnet(ctx, work.ID, candidates[0].ID); err != nil {
		t.Fatalf("select candidate: %v", err)
	}
	batch, err := CreateJavDownloadBatch(ctx, []int64{work.ID})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	attemptID := batch.Attempts[0].ID
	if _, err := MarkJavDownloadAttempt(ctx, attemptID, models.JavDownloadAttemptDownloaded, "task-order", ""); err != nil {
		t.Fatalf("mark downloaded: %v", err)
	}
	stale, err := MarkJavDownloadAttempt(ctx, attemptID, models.JavDownloadAttemptSubmitted, "task-order", "late callback")
	if err != nil {
		t.Fatalf("apply stale callback: %v", err)
	}
	if stale.Status != models.JavDownloadAttemptDownloaded {
		t.Fatalf("stale callback status=%q, want downloaded", stale.Status)
	}
	awaiting, err := MarkJavDownloadAttempt(ctx, attemptID, models.JavDownloadAttemptAwaitingQuality, "task-order", "")
	if err != nil {
		t.Fatalf("mark awaiting quality: %v", err)
	}
	terminal, err := MarkJavDownloadAttempt(ctx, attemptID, models.JavDownloadAttemptFailed, "task-order", "late failure")
	if err != nil {
		t.Fatalf("apply stale failure callback: %v", err)
	}
	if terminal.Status != models.JavDownloadAttemptAwaitingQuality || terminal.Error != awaiting.Error {
		t.Fatalf("stale failure callback changed attempt: before=%#v after=%#v", awaiting, terminal)
	}
}

func TestActiveDownloadPreventsSwitchingSelectedMagnet(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	work := models.Jav{Code: "MAG-ACTIVE-001", NormalizedCode: "MAGACTIVE001"}
	if err := database.Create(&work).Error; err != nil {
		t.Fatalf("create JAV: %v", err)
	}
	candidates, err := UpsertJavMagnetCandidates(ctx, work.ID, []jav.JavDBAppMagnet{
		{Hash: "active-first", Name: "first"},
		{Hash: "active-second", Name: "second"},
	})
	if err != nil || len(candidates) != 2 {
		t.Fatalf("upsert candidates=%#v err=%v", candidates, err)
	}
	if _, err := SelectJavMagnet(ctx, work.ID, candidates[0].ID); err != nil {
		t.Fatalf("select first candidate: %v", err)
	}
	batch, err := CreateJavDownloadBatch(ctx, []int64{work.ID})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if _, err := MarkJavDownloadAttempt(ctx, batch.Attempts[0].ID, models.JavDownloadAttemptSubmitted, "active-task", ""); err != nil {
		t.Fatalf("mark submitted: %v", err)
	}
	if _, err := SelectJavMagnet(ctx, work.ID, candidates[1].ID); !errors.Is(err, ErrJavDownloadAlreadyActive) {
		t.Fatalf("switch candidate error=%v, want %v", err, ErrJavDownloadAlreadyActive)
	}
	if _, err := SelectJavMagnet(ctx, work.ID, candidates[0].ID); err != nil {
		t.Fatalf("idempotently reselect active candidate: %v", err)
	}
	var acquisition models.JavAcquisition
	if err := database.First(&acquisition, work.ID).Error; err != nil {
		t.Fatalf("load acquisition: %v", err)
	}
	if acquisition.Stage != models.JavAcquisitionStageDownloadSubmitted {
		t.Fatalf("stage=%q, want download_submitted", acquisition.Stage)
	}
}

func TestDownloadQueueAndSubmitExcludeWorksWithExistingFiles(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	work := models.Jav{Code: "MAG-STORED-001", NormalizedCode: "MAGSTORED001"}
	if err := database.Create(&work).Error; err != nil {
		t.Fatalf("create JAV: %v", err)
	}
	candidates, err := UpsertJavMagnetCandidates(ctx, work.ID, []jav.JavDBAppMagnet{{Hash: "stored-hash"}})
	if err != nil {
		t.Fatalf("upsert candidate: %v", err)
	}
	if _, err := SelectJavMagnet(ctx, work.ID, candidates[0].ID); err != nil {
		t.Fatalf("select candidate: %v", err)
	}
	directory := models.Directory{Path: "/tmp/magnet-stored"}
	video := models.Video{Fingerprint: "magnet-stored-video"}
	if err := database.Create(&directory).Error; err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := database.Create(&video).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}
	location, err := UpsertVideoLocation(ctx, video.ID, directory.ID, "MAG-STORED-001.mp4", time.Now().UTC())
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	if err := SetVideoLocationJavIDForVideo(ctx, location.ID, video.ID, work.ID, location.UpdatedAt); err != nil {
		t.Fatalf("link JAV file: %v", err)
	}
	queue, total, err := ListJavDownloadQueue(ctx, 10, 0)
	if err != nil || total != 0 || len(queue) != 0 {
		t.Fatalf("queue total=%d items=%#v err=%v", total, queue, err)
	}
	if _, err := CreateJavDownloadBatch(ctx, []int64{work.ID}); !errors.Is(err, ErrJavAlreadyHasFile) {
		t.Fatalf("submit existing file error=%v, want %v", err, ErrJavAlreadyHasFile)
	}
}

func TestUncertainHandoffThatLaterLandsStillRequiresQualityAcceptance(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	work := models.Jav{Code: "MAG-LATE-001", NormalizedCode: "MAGLATE001"}
	if err := database.Create(&work).Error; err != nil {
		t.Fatalf("create JAV: %v", err)
	}
	candidates, err := UpsertJavMagnetCandidates(ctx, work.ID, []jav.JavDBAppMagnet{{Hash: "late-hash"}})
	if err != nil {
		t.Fatalf("upsert candidate: %v", err)
	}
	if _, err := SelectJavMagnet(ctx, work.ID, candidates[0].ID); err != nil {
		t.Fatalf("select candidate: %v", err)
	}
	batch, err := CreateJavDownloadBatch(ctx, []int64{work.ID})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	attempt := batch.Attempts[0]
	if _, err := MarkJavDownloadAttempt(ctx, attempt.ID, models.JavDownloadAttemptUncertain, "", "response lost"); err != nil {
		t.Fatalf("mark uncertain handoff: %v", err)
	}
	directory := models.Directory{Path: "/tmp/magnet-late"}
	video := models.Video{Fingerprint: "magnet-late-video"}
	if err := database.Create(&directory).Error; err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := database.Create(&video).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}
	location, err := UpsertVideoLocation(ctx, video.ID, directory.ID, "MAG-LATE-001.mp4", time.Now().UTC())
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	if err := SetVideoLocationJavIDForVideo(ctx, location.ID, video.ID, work.ID, location.UpdatedAt); err != nil {
		t.Fatalf("link late JAV file: %v", err)
	}
	loaded, err := GetJav(ctx, work.ID, nil)
	if err != nil {
		t.Fatalf("load late JAV: %v", err)
	}
	if loaded.InventoryState != models.JavInventoryImported || loaded.AcquisitionStage != models.JavAcquisitionStageQualityReview {
		t.Fatalf("late JAV inventory=%q stage=%q", loaded.InventoryState, loaded.AcquisitionStage)
	}
	acceptance, err := AcceptJavDownloadedWorkWithReview(ctx, work.ID, candidates[0].ID, 0, JavMagnetReviewInput{Notes: "late but verified"})
	if err != nil {
		t.Fatalf("accept late JAV: %v", err)
	}
	if acceptance.AttemptID == nil || *acceptance.AttemptID != attempt.ID {
		t.Fatalf("acceptance attempt=%v, want %d", acceptance.AttemptID, attempt.ID)
	}
	finalBatch, err := GetJavDownloadBatch(ctx, batch.ID)
	if err != nil || finalBatch.Status != models.JavDownloadBatchCompleted {
		t.Fatalf("final batch=%#v err=%v", finalBatch, err)
	}
}

func TestRejectedDownloadedFileNeverReconcilesAsFormalImport(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	work := models.Jav{Code: "MAG-RETAIN-001", NormalizedCode: "MAGRETAIN001"}
	if err := database.Create(&work).Error; err != nil {
		t.Fatalf("create JAV: %v", err)
	}
	candidates, err := UpsertJavMagnetCandidates(ctx, work.ID, []jav.JavDBAppMagnet{{Hash: "retain-hash"}})
	if err != nil {
		t.Fatalf("upsert candidate: %v", err)
	}
	if _, err := SelectJavMagnet(ctx, work.ID, candidates[0].ID); err != nil {
		t.Fatalf("select candidate: %v", err)
	}
	batch, err := CreateJavDownloadBatch(ctx, []int64{work.ID})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if _, err := MarkJavDownloadAttempt(ctx, batch.Attempts[0].ID, models.JavDownloadAttemptAwaitingQuality, "retain-task", ""); err != nil {
		t.Fatalf("mark awaiting quality: %v", err)
	}
	directory := models.Directory{Path: "/tmp/magnet-retain"}
	video := models.Video{Fingerprint: "magnet-retain-video"}
	if err := database.Create(&directory).Error; err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := database.Create(&video).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}
	location, err := UpsertVideoLocation(ctx, video.ID, directory.ID, "MAG-RETAIN-001.mp4", time.Now().UTC())
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	if err := SetVideoLocationJavIDForVideo(ctx, location.ID, video.ID, work.ID, location.UpdatedAt); err != nil {
		t.Fatalf("link JAV file: %v", err)
	}
	queue, total, err := ListJavQualityReviewQueue(ctx, 10, 0, nil)
	if err != nil || total != 1 || len(queue) != 1 || queue[0].ID != work.ID {
		t.Fatalf("quality queue total=%d items=%#v err=%v", total, queue, err)
	}
	if _, err := RejectJavDownloadedWork(ctx, work.ID, candidates[0].ID, batch.Attempts[0].ID, []string{"watermark"}, "retained for inspection"); err != nil {
		t.Fatalf("reject candidate: %v", err)
	}
	if err := database.Transaction(func(tx *gorm.DB) error {
		return reconcileJavAcquisitionStagesTx(tx, []int64{work.ID})
	}); err != nil {
		t.Fatalf("reconcile retained rejected file: %v", err)
	}
	loaded, err := GetJav(ctx, work.ID, nil)
	if err != nil {
		t.Fatalf("load retained rejected JAV: %v", err)
	}
	if loaded.InventoryState != models.JavInventoryImported || loaded.AcquisitionStage != models.JavAcquisitionStageMagnetReview {
		t.Fatalf("retained rejected JAV inventory=%q stage=%q", loaded.InventoryState, loaded.AcquisitionStage)
	}
	queue, total, err = ListJavQualityReviewQueue(ctx, 10, 0, nil)
	if err != nil || total != 0 || len(queue) != 0 {
		t.Fatalf("quality queue after rejection total=%d items=%#v err=%v", total, queue, err)
	}
}

func TestQualityAcceptanceRefusesToGuessAmongMultipleLocations(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	work := models.Jav{Code: "MAG-MIRROR-001", NormalizedCode: "MAGMIRROR001"}
	if err := database.Create(&work).Error; err != nil {
		t.Fatalf("create JAV: %v", err)
	}
	candidates, err := UpsertJavMagnetCandidates(ctx, work.ID, []jav.JavDBAppMagnet{{Hash: "mirror-hash"}})
	if err != nil {
		t.Fatalf("upsert candidate: %v", err)
	}
	if _, err := SelectJavMagnet(ctx, work.ID, candidates[0].ID); err != nil {
		t.Fatalf("select candidate: %v", err)
	}
	batch, err := CreateJavDownloadBatch(ctx, []int64{work.ID})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if _, err := MarkJavDownloadAttempt(ctx, batch.Attempts[0].ID, models.JavDownloadAttemptSubmitted, "mirror-task", ""); err != nil {
		t.Fatalf("mark submitted: %v", err)
	}
	directories := []models.Directory{{Path: "/tmp/magnet-mirror-a"}, {Path: "/tmp/magnet-mirror-b"}}
	video := models.Video{Fingerprint: "magnet-mirror-video"}
	if err := database.Create(&directories).Error; err != nil {
		t.Fatalf("create directories: %v", err)
	}
	if err := database.Create(&video).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}
	for index := range directories {
		location, err := UpsertVideoLocation(ctx, video.ID, directories[index].ID, fmt.Sprintf("mirror-%d/MAG-MIRROR-001.mp4", index), time.Now().UTC())
		if err != nil {
			t.Fatalf("create mirror %d: %v", index, err)
		}
		if err := SetVideoLocationJavIDForVideo(ctx, location.ID, video.ID, work.ID, location.UpdatedAt); err != nil {
			t.Fatalf("link mirror %d: %v", index, err)
		}
	}
	if _, err := AcceptJavDownloadedWork(ctx, work.ID, candidates[0].ID, 0, ""); !errors.Is(err, ErrJavDownloadAmbiguousFile) {
		t.Fatalf("accept mirrors error=%v, want %v", err, ErrJavDownloadAmbiguousFile)
	}
}

func TestJavMagnetRejectionIsRetainedAndAcceptanceRequiresFile(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	work := models.Jav{Code: "MAG-002", NormalizedCode: "MAG002", Title: "Review work"}
	if err := database.Create(&work).Error; err != nil {
		t.Fatalf("create JAV: %v", err)
	}
	created, err := UpsertJavMagnetCandidates(ctx, work.ID, []jav.JavDBAppMagnet{{Hash: "review-hash", Name: "review"}})
	if err != nil {
		t.Fatalf("upsert magnet: %v", err)
	}
	if _, err := RejectJavDownloadedWork(ctx, work.ID, created[0].ID, 0, []string{"watermark"}, "visible watermark"); err != nil {
		t.Fatalf("reject magnet: %v", err)
	}
	all, err := ListJavMagnetCandidates(ctx, work.ID, true)
	if err != nil || len(all) != 1 || all[0].ReviewStatus != models.JavMagnetReviewRejected || all[0].ReviewReasons != "watermark" {
		t.Fatalf("rejected candidates=%#v err=%v", all, err)
	}
	if _, err := AcceptJavDownloadedWork(ctx, work.ID, created[0].ID, 0, ""); err != ErrJavDownloadNoFile {
		t.Fatalf("acceptance without file error=%v, want %v", err, ErrJavDownloadNoFile)
	}
	if _, err := ReviewJavMagnet(ctx, work.ID, created[0].ID, JavMagnetReviewInput{Accepted: true}); err == nil {
		t.Fatal("candidate-only review must not create an accepted verdict")
	}

	// Keep the test explicit about timestamps used by later daily aggregation.
	_ = time.Now().UTC()
}

func TestRejectedMagnetStoresStructuredFactsAndClearsSelection(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	work := models.Jav{Code: "MAG-FACT-001", NormalizedCode: "MAGFACT001"}
	if err := database.Create(&work).Error; err != nil {
		t.Fatalf("create JAV: %v", err)
	}
	candidates, err := UpsertJavMagnetCandidates(ctx, work.ID, []jav.JavDBAppMagnet{{Hash: "fact-hash"}})
	if err != nil {
		t.Fatalf("upsert magnet: %v", err)
	}
	if _, err := SelectJavMagnet(ctx, work.ID, candidates[0].ID); err != nil {
		t.Fatalf("select magnet: %v", err)
	}
	no, yes := false, true
	rejected, err := RejectJavDownloadedWorkWithReview(ctx, work.ID, candidates[0].ID, 0, JavMagnetReviewInput{
		QualityClear: &no, Confirmed1080P: &no, HasWatermark: &yes,
		Reasons: []string{"low_clarity", "watermark"}, Notes: "visible mark",
	})
	if err != nil {
		t.Fatalf("reject structured review: %v", err)
	}
	if rejected.QualityClear == nil || *rejected.QualityClear || rejected.HasWatermark == nil || !*rejected.HasWatermark {
		t.Fatalf("structured review=%#v", rejected)
	}
	var selection models.JavMagnetSelection
	if err := database.Where("jav_id = ?", work.ID).First(&selection).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("selection after rejection error=%v selection=%#v", err, selection)
	}
}

func TestJavImportDaysContainOnlyAcceptedDatesAndCanonicalItems(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	works := []models.Jav{
		{Code: "DAY-001", NormalizedCode: "DAY001"},
		{Code: "DAY-002", NormalizedCode: "DAY002"},
	}
	if err := database.Create(&works).Error; err != nil {
		t.Fatalf("create JAV works: %v", err)
	}
	acceptedAt := time.Date(2026, 8, 27, 18, 0, 0, 0, time.UTC)
	acceptance := models.JavQualityAcceptance{
		JavID: works[0].ID, CandidateID: 77, AcceptedAt: acceptedAt,
		CreatedAt: acceptedAt, UpdatedAt: acceptedAt,
	}
	if err := database.Create(&acceptance).Error; err != nil {
		t.Fatalf("create acceptance: %v", err)
	}
	days, total, err := ListJavImportDaySummaries(ctx, 31, 0, nil)
	if err != nil {
		t.Fatalf("list import days: %v", err)
	}
	wantDay := acceptedAt.In(time.Local).Format("2006-01-02")
	if total != 1 || len(days) != 1 || days[0].Day != wantDay || days[0].Count != 1 || len(days[0].Items) != 1 || days[0].Items[0].ID != works[0].ID {
		t.Fatalf("days=%#v total=%d", days, total)
	}
}
