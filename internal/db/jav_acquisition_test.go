package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"javboss/internal/models"
)

func TestUpsertVideoLocationReplacementReconcilesImportedAcquisition(t *testing.T) {
	gdb := openTestDB(t)
	ctx := context.Background()
	now := time.Unix(1710000000, 0).UTC()

	dir := models.Directory{Path: "/tmp/reconcile-replacement"}
	if err := gdb.Create(&dir).Error; err != nil {
		t.Fatalf("create directory: %v", err)
	}
	oldVideo := models.Video{Fingerprint: "reconcile-old-video", DurationSec: 120}
	newVideo := models.Video{Fingerprint: "reconcile-new-video", DurationSec: 120}
	if err := gdb.Create(&oldVideo).Error; err != nil {
		t.Fatalf("create old video: %v", err)
	}
	if err := gdb.Create(&newVideo).Error; err != nil {
		t.Fatalf("create new video: %v", err)
	}
	javRec := models.Jav{Code: "RECON-001", Title: "Replacement work"}
	if err := gdb.Create(&javRec).Error; err != nil {
		t.Fatalf("create JAV: %v", err)
	}
	loc, err := UpsertVideoLocation(ctx, oldVideo.ID, dir.ID, "same.mp4", now)
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	if err := gdb.Model(&models.VideoLocation{}).Where("id = ?", loc.ID).Update("jav_id", javRec.ID).Error; err != nil {
		t.Fatalf("link location: %v", err)
	}
	if err := gdb.Create(&models.JavAcquisition{
		JavID: javRec.ID, Stage: models.JavAcquisitionStageImported, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create acquisition: %v", err)
	}

	replaced, err := UpsertVideoLocation(ctx, newVideo.ID, dir.ID, "same.mp4", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("replace location: %v", err)
	}
	if replaced.JavID != nil {
		t.Fatalf("replacement retained old JAV link: %#v", replaced.JavID)
	}
	var acquisition models.JavAcquisition
	if err := gdb.First(&acquisition, javRec.ID).Error; err != nil {
		t.Fatalf("reload acquisition: %v", err)
	}
	if acquisition.Stage != models.JavAcquisitionStageMagnetCollecting {
		t.Fatalf("replacement stage = %q, want magnet_collecting", acquisition.Stage)
	}
}

func TestDirectoryRestoreRejectsDifferentHiddenMediaForSameJav(t *testing.T) {
	gdb := openTestDB(t)
	ctx := context.Background()
	now := time.Unix(1710000000, 0).UTC()

	dirs := []models.Directory{{Path: "/tmp/reconcile-conflict-hidden-a"}, {Path: "/tmp/reconcile-conflict-hidden-b"}}
	if err := gdb.Create(&dirs).Error; err != nil {
		t.Fatalf("create directories: %v", err)
	}
	videos := []models.Video{
		{Fingerprint: "reconcile-conflict-hidden-old", DurationSec: 120},
		{Fingerprint: "reconcile-conflict-hidden-new", DurationSec: 120},
	}
	if err := gdb.Create(&videos).Error; err != nil {
		t.Fatalf("create videos: %v", err)
	}
	javRec := models.Jav{Code: "RECON-CONFLICT-HIDDEN-001", Title: "Conflict work"}
	if err := gdb.Create(&javRec).Error; err != nil {
		t.Fatalf("create JAV: %v", err)
	}
	if err := gdb.Create(&models.JavAcquisition{
		JavID: javRec.ID, Stage: models.JavAcquisitionStageMetadataPending, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create acquisition: %v", err)
	}
	oldLoc, err := UpsertVideoLocation(ctx, videos[0].ID, dirs[0].ID, "old/movie.mp4", now)
	if err != nil {
		t.Fatalf("create old location: %v", err)
	}
	if err := SetVideoLocationJavIDForVideo(ctx, oldLoc.ID, videos[0].ID, javRec.ID, oldLoc.UpdatedAt); err != nil {
		t.Fatalf("link old media: %v", err)
	}
	if _, err := SetDirectoryDeletedAndHideVideos(ctx, dirs[0].ID, true); err != nil {
		t.Fatalf("hide old directory: %v", err)
	}

	newLoc, err := UpsertVideoLocation(ctx, videos[1].ID, dirs[1].ID, "new/movie.mp4", now)
	if err != nil {
		t.Fatalf("create new location: %v", err)
	}
	// The old location is hidden, so linking the replacement is allowed while
	// it is the only active media asset.
	if err := SetVideoLocationJavIDForVideo(ctx, newLoc.ID, videos[1].ID, javRec.ID, newLoc.UpdatedAt); err != nil {
		t.Fatalf("link replacement media: %v", err)
	}

	if _, err := SetDirectoryDeletedAndHideVideos(ctx, dirs[0].ID, false); !errors.Is(err, ErrJavMediaConflict) {
		t.Fatalf("restore hidden conflicting directory error = %v, want ErrJavMediaConflict", err)
	}
	var storedDir models.Directory
	if err := gdb.First(&storedDir, dirs[0].ID).Error; err != nil {
		t.Fatalf("reload hidden directory: %v", err)
	}
	if !storedDir.IsDelete {
		t.Fatal("conflicting directory restore should have been rolled back")
	}
}

func TestUpdateVideoLocationPathReconcilesDeletedHiddenDestinationJav(t *testing.T) {
	gdb := openTestDB(t)
	ctx := context.Background()
	now := time.Unix(1710000000, 0).UTC()
	dir := models.Directory{Path: "/tmp/reconcile-hidden-destination"}
	if err := gdb.Create(&dir).Error; err != nil {
		t.Fatalf("create directory: %v", err)
	}
	videos := []models.Video{
		{Fingerprint: "reconcile-hidden-destination-old", DurationSec: 120},
		{Fingerprint: "reconcile-hidden-destination-current", DurationSec: 120},
	}
	if err := gdb.Create(&videos).Error; err != nil {
		t.Fatalf("create videos: %v", err)
	}
	javRec := models.Jav{Code: "RECON-HIDDEN-DESTINATION-001", Title: "Hidden destination work"}
	if err := gdb.Create(&javRec).Error; err != nil {
		t.Fatalf("create JAV: %v", err)
	}
	if err := gdb.Create(&models.JavAcquisition{
		JavID: javRec.ID, Stage: models.JavAcquisitionStageImported, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create acquisition: %v", err)
	}
	hidden, err := UpsertVideoLocation(ctx, videos[0].ID, dir.ID, "target/movie.mp4", now)
	if err != nil {
		t.Fatalf("create hidden destination: %v", err)
	}
	if err := gdb.Model(&models.VideoLocation{}).Where("id = ?", hidden.ID).Updates(map[string]any{
		"jav_id": javRec.ID, "is_delete": true,
	}).Error; err != nil {
		t.Fatalf("hide destination location: %v", err)
	}
	current, err := UpsertVideoLocation(ctx, videos[1].ID, dir.ID, "source/movie.mp4", now)
	if err != nil {
		t.Fatalf("create current location: %v", err)
	}
	if _, err := UpdateVideoLocationPath(ctx, current.ID, "target/movie.mp4", now.Add(time.Minute)); err != nil {
		t.Fatalf("move current location over hidden destination: %v", err)
	}
	var acquisition models.JavAcquisition
	if err := gdb.First(&acquisition, javRec.ID).Error; err != nil {
		t.Fatalf("reload acquisition: %v", err)
	}
	if acquisition.Stage != models.JavAcquisitionStageMagnetCollecting {
		t.Fatalf("stage after deleting hidden last location = %q, want magnet_collecting", acquisition.Stage)
	}
}

func TestHideVideoLocationsReconcilesLastLocationAndPreservesMirrors(t *testing.T) {
	gdb := openTestDB(t)
	ctx := context.Background()
	now := time.Unix(1710000000, 0).UTC()

	dirs := []models.Directory{{Path: "/tmp/reconcile-hide-a"}, {Path: "/tmp/reconcile-hide-b"}}
	if err := gdb.Create(&dirs).Error; err != nil {
		t.Fatalf("create directories: %v", err)
	}
	video := models.Video{Fingerprint: "reconcile-hide-video", DurationSec: 120}
	if err := gdb.Create(&video).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}
	javRec := models.Jav{Code: "RECON-002", Title: "Mirror work"}
	if err := gdb.Create(&javRec).Error; err != nil {
		t.Fatalf("create JAV: %v", err)
	}
	locA, err := UpsertVideoLocation(ctx, video.ID, dirs[0].ID, "a/movie.mp4", now)
	if err != nil {
		t.Fatalf("create location A: %v", err)
	}
	locB, err := UpsertVideoLocation(ctx, video.ID, dirs[1].ID, "b/movie.mp4", now)
	if err != nil {
		t.Fatalf("create location B: %v", err)
	}
	if err := gdb.Model(&models.VideoLocation{}).Where("id IN ?", []int64{locA.ID, locB.ID}).Update("jav_id", javRec.ID).Error; err != nil {
		t.Fatalf("link mirror locations: %v", err)
	}
	if err := gdb.Create(&models.JavAcquisition{
		JavID: javRec.ID, Stage: models.JavAcquisitionStageImported, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create acquisition: %v", err)
	}

	if err := HideVideoLocationsByIDs(ctx, []int64{locA.ID}); err != nil {
		t.Fatalf("hide first mirror: %v", err)
	}
	var acquisition models.JavAcquisition
	if err := gdb.First(&acquisition, javRec.ID).Error; err != nil {
		t.Fatalf("reload acquisition after first hide: %v", err)
	}
	if acquisition.Stage != models.JavAcquisitionStageImported {
		t.Fatalf("stage after first mirror hide = %q, want imported", acquisition.Stage)
	}

	if err := HideVideoLocationsByIDs(ctx, []int64{locB.ID}); err != nil {
		t.Fatalf("hide last mirror: %v", err)
	}
	if err := gdb.First(&acquisition, javRec.ID).Error; err != nil {
		t.Fatalf("reload acquisition after last hide: %v", err)
	}
	if acquisition.Stage != models.JavAcquisitionStageMagnetCollecting {
		t.Fatalf("stage after last mirror hide = %q, want magnet_collecting", acquisition.Stage)
	}
}

func TestClearVideoLocationJavIDReconcilesAcquisitionAndDoesNotRewindUnimported(t *testing.T) {
	gdb := openTestDB(t)
	ctx := context.Background()
	now := time.Unix(1710000000, 0).UTC()

	dir := models.Directory{Path: "/tmp/reconcile-clear"}
	if err := gdb.Create(&dir).Error; err != nil {
		t.Fatalf("create directory: %v", err)
	}
	video := models.Video{Fingerprint: "reconcile-clear-video", DurationSec: 120}
	if err := gdb.Create(&video).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}
	importedJav := models.Jav{Code: "RECON-003", Title: "Imported clear"}
	unimportedJav := models.Jav{Code: "RECON-004", Title: "Unimported clear"}
	if err := gdb.Create(&importedJav).Error; err != nil {
		t.Fatalf("create imported JAV: %v", err)
	}
	if err := gdb.Create(&unimportedJav).Error; err != nil {
		t.Fatalf("create unimported JAV: %v", err)
	}
	locImported, err := UpsertVideoLocation(ctx, video.ID, dir.ID, "imported.mp4", now)
	if err != nil {
		t.Fatalf("create imported location: %v", err)
	}
	locUnimported, err := UpsertVideoLocation(ctx, video.ID, dir.ID, "unimported.mp4", now)
	if err != nil {
		t.Fatalf("create unimported location: %v", err)
	}
	if err := gdb.Model(&models.VideoLocation{}).Where("id = ?", locImported.ID).Update("jav_id", importedJav.ID).Error; err != nil {
		t.Fatalf("link imported location: %v", err)
	}
	if err := gdb.Model(&models.VideoLocation{}).Where("id = ?", locUnimported.ID).Update("jav_id", unimportedJav.ID).Error; err != nil {
		t.Fatalf("link unimported location: %v", err)
	}
	if err := gdb.Create(&[]models.JavAcquisition{
		{JavID: importedJav.ID, Stage: models.JavAcquisitionStageImported, CreatedAt: now, UpdatedAt: now},
		{JavID: unimportedJav.ID, Stage: models.JavAcquisitionStageMetadataPending, CreatedAt: now, UpdatedAt: now},
	}).Error; err != nil {
		t.Fatalf("create acquisitions: %v", err)
	}

	if err := ClearVideoLocationJavIDForVideo(ctx, locImported.ID, video.ID, time.Time{}); err != nil {
		t.Fatalf("clear imported location: %v", err)
	}
	if err := ClearVideoLocationJavIDForVideo(ctx, locUnimported.ID, video.ID, time.Time{}); err != nil {
		t.Fatalf("clear unimported location: %v", err)
	}
	var stages []models.JavAcquisition
	if err := gdb.Order("jav_id").Find(&stages).Error; err != nil {
		t.Fatalf("reload acquisitions: %v", err)
	}
	if len(stages) != 2 {
		t.Fatalf("acquisition count = %d, want 2", len(stages))
	}
	if stages[0].Stage != models.JavAcquisitionStageMagnetCollecting ||
		stages[1].Stage != models.JavAcquisitionStageMetadataPending {
		t.Fatalf("clear stages = %#v, want magnet_collecting/metadata_pending", stages)
	}
}

func TestClearVideoLocationJavIDWithoutMetadataResumesMetadataCollection(t *testing.T) {
	gdb := openTestDB(t)
	ctx := context.Background()
	now := time.Unix(1710000000, 0).UTC()

	dir := models.Directory{Path: "/tmp/reconcile-clear-bare"}
	video := models.Video{Fingerprint: "reconcile-clear-bare-video", DurationSec: 120}
	javRec := models.Jav{Code: "RECON-BARE-001"}
	if err := gdb.Create(&dir).Error; err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := gdb.Create(&video).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}
	if err := gdb.Create(&javRec).Error; err != nil {
		t.Fatalf("create JAV: %v", err)
	}
	loc, err := UpsertVideoLocation(ctx, video.ID, dir.ID, "bare.mp4", now)
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	if err := gdb.Model(&models.VideoLocation{}).Where("id = ?", loc.ID).Update("jav_id", javRec.ID).Error; err != nil {
		t.Fatalf("link location: %v", err)
	}
	if err := gdb.Create(&models.JavAcquisition{
		JavID: javRec.ID, Stage: models.JavAcquisitionStageImported, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create acquisition: %v", err)
	}

	if err := ClearVideoLocationJavIDForVideo(ctx, loc.ID, video.ID, time.Time{}); err != nil {
		t.Fatalf("clear location: %v", err)
	}
	var acquisition models.JavAcquisition
	if err := gdb.First(&acquisition, javRec.ID).Error; err != nil {
		t.Fatalf("reload acquisition: %v", err)
	}
	if acquisition.Stage != models.JavAcquisitionStageMetadataPending {
		t.Fatalf("bare acquisition stage = %q, want metadata_pending", acquisition.Stage)
	}
}

func TestSetVideoLocationJavIDReassignmentReconcilesOldAndNewWorks(t *testing.T) {
	gdb := openTestDB(t)
	ctx := context.Background()
	now := time.Unix(1710000000, 0).UTC()

	dir := models.Directory{Path: "/tmp/reconcile-reassignment"}
	video := models.Video{Fingerprint: "reconcile-reassignment-video", DurationSec: 120}
	works := []models.Jav{
		{Code: "RECON-OLD-001", Title: "Old scraped work"},
		{Code: "RECON-NEW-001", Title: "New scraped work"},
	}
	if err := gdb.Create(&dir).Error; err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := gdb.Create(&video).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}
	if err := gdb.Create(&works).Error; err != nil {
		t.Fatalf("create JAVs: %v", err)
	}
	loc, err := UpsertVideoLocation(ctx, video.ID, dir.ID, "movie.mp4", now)
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	if err := gdb.Model(&models.VideoLocation{}).
		Where("id = ?", loc.ID).
		Update("jav_id", works[0].ID).Error; err != nil {
		t.Fatalf("link old JAV: %v", err)
	}
	if err := gdb.Create(&[]models.JavAcquisition{
		{JavID: works[0].ID, Stage: models.JavAcquisitionStageImported, CreatedAt: now, UpdatedAt: now},
		{JavID: works[1].ID, Stage: models.JavAcquisitionStageMagnetCollecting, CreatedAt: now, UpdatedAt: now},
	}).Error; err != nil {
		t.Fatalf("create acquisitions: %v", err)
	}

	if err := SetVideoLocationJavID(ctx, loc.ID, works[1].ID, time.Time{}); err != nil {
		t.Fatalf("reassign location: %v", err)
	}
	var acquisitions []models.JavAcquisition
	if err := gdb.Order("jav_id").Find(&acquisitions).Error; err != nil {
		t.Fatalf("reload acquisitions: %v", err)
	}
	if len(acquisitions) != 2 ||
		acquisitions[0].Stage != models.JavAcquisitionStageMagnetCollecting ||
		acquisitions[1].Stage != models.JavAcquisitionStageImported {
		t.Fatalf("reassignment stages = %#v, want magnet_collecting/imported", acquisitions)
	}
}

func TestDeleteByIDsReconcilesCascadedLocationsAndPreservesOtherActiveLocation(t *testing.T) {
	gdb := openTestDB(t)
	ctx := context.Background()
	now := time.Unix(1710000000, 0).UTC()

	dirs := []models.Directory{{Path: "/tmp/reconcile-delete-a"}, {Path: "/tmp/reconcile-delete-b"}}
	videos := []models.Video{
		{Fingerprint: "reconcile-delete-target", DurationSec: 120},
		{Fingerprint: "reconcile-delete-mirror", DurationSec: 120},
	}
	javs := []models.Jav{
		{Code: "RECON-DEL-001", Title: "Last location"},
		{Code: "RECON-DEL-002", Title: "Mirrored location"},
	}
	if err := gdb.Create(&dirs).Error; err != nil {
		t.Fatalf("create directories: %v", err)
	}
	if err := gdb.Create(&videos).Error; err != nil {
		t.Fatalf("create videos: %v", err)
	}
	if err := gdb.Create(&javs).Error; err != nil {
		t.Fatalf("create JAVs: %v", err)
	}
	last, err := UpsertVideoLocation(ctx, videos[0].ID, dirs[0].ID, "last.mp4", now)
	if err != nil {
		t.Fatalf("create last location: %v", err)
	}
	removedMirror, err := UpsertVideoLocation(ctx, videos[0].ID, dirs[1].ID, "removed-mirror.mp4", now)
	if err != nil {
		t.Fatalf("create removed mirror: %v", err)
	}
	keptMirror, err := UpsertVideoLocation(ctx, videos[1].ID, dirs[1].ID, "kept-mirror.mp4", now)
	if err != nil {
		t.Fatalf("create kept mirror: %v", err)
	}
	if err := gdb.Model(&models.VideoLocation{}).Where("id = ?", last.ID).Update("jav_id", javs[0].ID).Error; err != nil {
		t.Fatalf("link last location: %v", err)
	}
	if err := gdb.Model(&models.VideoLocation{}).
		Where("id IN ?", []int64{removedMirror.ID, keptMirror.ID}).
		Update("jav_id", javs[1].ID).Error; err != nil {
		t.Fatalf("link mirror locations: %v", err)
	}
	if err := gdb.Create(&[]models.JavAcquisition{
		{JavID: javs[0].ID, Stage: models.JavAcquisitionStageImported, CreatedAt: now, UpdatedAt: now},
		{JavID: javs[1].ID, Stage: models.JavAcquisitionStageImported, CreatedAt: now, UpdatedAt: now},
	}).Error; err != nil {
		t.Fatalf("create acquisitions: %v", err)
	}

	if err := DeleteByIDs(ctx, []int64{videos[0].ID}); err != nil {
		t.Fatalf("delete target video: %v", err)
	}
	var acquisitions []models.JavAcquisition
	if err := gdb.Order("jav_id").Find(&acquisitions).Error; err != nil {
		t.Fatalf("reload acquisitions: %v", err)
	}
	if len(acquisitions) != 2 {
		t.Fatalf("acquisition count = %d, want 2", len(acquisitions))
	}
	if acquisitions[0].Stage != models.JavAcquisitionStageMagnetCollecting ||
		acquisitions[1].Stage != models.JavAcquisitionStageImported {
		t.Fatalf("delete stages = %#v, want magnet_collecting/imported", acquisitions)
	}
}

func TestDirectoryVisibilityReconcilesImportedStage(t *testing.T) {
	gdb := openTestDB(t)
	ctx := context.Background()
	now := time.Unix(1710000000, 0).UTC()

	dir := models.Directory{Path: "/tmp/reconcile-directory"}
	if err := gdb.Create(&dir).Error; err != nil {
		t.Fatalf("create directory: %v", err)
	}
	video := models.Video{Fingerprint: "reconcile-directory-video", DurationSec: 120}
	if err := gdb.Create(&video).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}
	javRec := models.Jav{Code: "RECON-005", Title: "Directory work"}
	if err := gdb.Create(&javRec).Error; err != nil {
		t.Fatalf("create JAV: %v", err)
	}
	loc, err := UpsertVideoLocation(ctx, video.ID, dir.ID, "movie.mp4", now)
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	if err := gdb.Model(&models.VideoLocation{}).Where("id = ?", loc.ID).Update("jav_id", javRec.ID).Error; err != nil {
		t.Fatalf("link location: %v", err)
	}
	if err := gdb.Create(&models.JavAcquisition{
		JavID: javRec.ID, Stage: models.JavAcquisitionStageImported, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create acquisition: %v", err)
	}

	if _, err := SetDirectoryDeletedAndHideVideos(ctx, dir.ID, true); err != nil {
		t.Fatalf("hide directory: %v", err)
	}
	var acquisition models.JavAcquisition
	if err := gdb.First(&acquisition, javRec.ID).Error; err != nil {
		t.Fatalf("reload hidden acquisition: %v", err)
	}
	if acquisition.Stage != models.JavAcquisitionStageMagnetCollecting {
		t.Fatalf("hidden directory stage = %q, want magnet_collecting", acquisition.Stage)
	}

	if _, err := SetDirectoryDeletedAndHideVideos(ctx, dir.ID, false); err != nil {
		t.Fatalf("restore directory: %v", err)
	}
	if err := gdb.First(&acquisition, javRec.ID).Error; err != nil {
		t.Fatalf("reload restored acquisition: %v", err)
	}
	if acquisition.Stage != models.JavAcquisitionStageImported {
		t.Fatalf("restored directory stage = %q, want imported", acquisition.Stage)
	}
}

func TestReconcileJavAcquisitionIgnoresMissingDirectory(t *testing.T) {
	gdb := openTestDB(t)
	ctx := context.Background()
	now := time.Unix(1710000000, 0).UTC()

	dir := models.Directory{Path: "/tmp/reconcile-missing", Missing: true}
	if err := gdb.Create(&dir).Error; err != nil {
		t.Fatalf("create directory: %v", err)
	}
	video := models.Video{Fingerprint: "reconcile-missing-video", DurationSec: 120}
	if err := gdb.Create(&video).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}
	javRec := models.Jav{Code: "RECON-006", Title: "Missing work"}
	if err := gdb.Create(&javRec).Error; err != nil {
		t.Fatalf("create JAV: %v", err)
	}
	loc, err := UpsertVideoLocation(ctx, video.ID, dir.ID, "movie.mp4", now)
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	if err := gdb.Model(&models.VideoLocation{}).Where("id = ?", loc.ID).Update("jav_id", javRec.ID).Error; err != nil {
		t.Fatalf("link location: %v", err)
	}
	if err := gdb.Create(&models.JavAcquisition{
		JavID: javRec.ID, Stage: models.JavAcquisitionStageImported, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create acquisition: %v", err)
	}
	if err := SetDirectoryMissing(ctx, dir.ID, true); err != nil {
		t.Fatalf("set missing: %v", err)
	}
	var acquisition models.JavAcquisition
	if err := gdb.First(&acquisition, javRec.ID).Error; err != nil {
		t.Fatalf("reload acquisition: %v", err)
	}
	if acquisition.Stage != models.JavAcquisitionStageImported {
		t.Fatalf("missing directory stage = %q, want imported", acquisition.Stage)
	}
}
