package db

import (
	"context"
	"testing"
	"time"

	"javboss/internal/models"
)

func TestVideoLocationPathExistsIgnoresHiddenRows(t *testing.T) {
	gdb := openTestDB(t)
	ctx := context.Background()
	now := time.Unix(1710000000, 0).UTC()

	dir := models.Directory{Path: "/tmp/media"}
	if err := gdb.Create(&dir).Error; err != nil {
		t.Fatalf("create directory: %v", err)
	}
	video := models.Video{
		DirectoryID: dir.ID,
		Path:        "deleted.mp4",
		Filename:    "deleted.mp4",
		Fingerprint: "hidden-path-exists-fp",
		ModifiedAt:  now,
		DurationSec: 1,
	}
	if err := gdb.Create(&video).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}
	loc, err := UpsertVideoLocation(ctx, video.ID, dir.ID, "deleted.mp4", now)
	if err != nil {
		t.Fatalf("upsert location: %v", err)
	}
	if err := HideVideoLocationsByIDs(ctx, []int64{loc.ID}); err != nil {
		t.Fatalf("hide location: %v", err)
	}

	exists, err := VideoLocationPathExists(ctx, dir.ID, "deleted.mp4")
	if err != nil {
		t.Fatalf("check path exists: %v", err)
	}
	if exists {
		t.Fatal("hidden location should not reserve its path for rename conflict checks")
	}
}

func TestUpdateVideoLocationPathReusesHiddenPath(t *testing.T) {
	gdb := openTestDB(t)
	ctx := context.Background()
	now := time.Unix(1710000000, 0).UTC()

	dir := models.Directory{Path: "/tmp/media"}
	if err := gdb.Create(&dir).Error; err != nil {
		t.Fatalf("create directory: %v", err)
	}
	hiddenVideo := models.Video{
		DirectoryID: dir.ID,
		Path:        "target.mp4",
		Filename:    "target.mp4",
		Fingerprint: "hidden-target-fp",
		ModifiedAt:  now,
	}
	activeVideo := models.Video{
		DirectoryID: dir.ID,
		Path:        "source.mp4",
		Filename:    "source.mp4",
		Fingerprint: "active-source-fp",
		ModifiedAt:  now,
	}
	if err := gdb.Create(&hiddenVideo).Error; err != nil {
		t.Fatalf("create hidden video: %v", err)
	}
	if err := gdb.Create(&activeVideo).Error; err != nil {
		t.Fatalf("create active video: %v", err)
	}
	hiddenLoc, err := UpsertVideoLocation(ctx, hiddenVideo.ID, dir.ID, "target.mp4", now)
	if err != nil {
		t.Fatalf("upsert hidden location: %v", err)
	}
	activeLoc, err := UpsertVideoLocation(ctx, activeVideo.ID, dir.ID, "source.mp4", now)
	if err != nil {
		t.Fatalf("upsert active location: %v", err)
	}
	if err := HideVideoLocationsByIDs(ctx, []int64{hiddenLoc.ID}); err != nil {
		t.Fatalf("hide target location: %v", err)
	}

	updated, err := UpdateVideoLocationPath(ctx, activeLoc.ID, "target.mp4", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("update active location path: %v", err)
	}
	if updated.ID != activeLoc.ID || updated.RelativePath != "target.mp4" || updated.Filename != "target.mp4" {
		t.Fatalf("unexpected updated location: %#v", updated)
	}

	var locations []models.VideoLocation
	if err := gdb.
		Where("directory_id = ? AND relative_path = ?", dir.ID, "target.mp4").
		Find(&locations).Error; err != nil {
		t.Fatalf("load target locations: %v", err)
	}
	if len(locations) != 1 {
		t.Fatalf("target path should have exactly one row after reuse: %#v", locations)
	}
	if locations[0].ID != activeLoc.ID || locations[0].IsDelete {
		t.Fatalf("target path should belong to the active location: %#v", locations[0])
	}
}

func TestUpsertVideoLocationClearsJavWhenPathPointsToDifferentVideo(t *testing.T) {
	gdb := openTestDB(t)
	ctx := context.Background()
	now := time.Unix(1710000000, 0).UTC()

	dir := models.Directory{Path: "/tmp/media"}
	if err := gdb.Create(&dir).Error; err != nil {
		t.Fatalf("create directory: %v", err)
	}
	oldVideo := models.Video{Fingerprint: "old-path-content", DurationSec: 3600}
	newVideo := models.Video{Fingerprint: "new-path-content", DurationSec: 3600}
	if err := gdb.Create(&oldVideo).Error; err != nil {
		t.Fatalf("create old video: %v", err)
	}
	if err := gdb.Create(&newVideo).Error; err != nil {
		t.Fatalf("create new video: %v", err)
	}
	javRec := models.Jav{Code: "TEST-001", Title: "Original work"}
	if err := gdb.Create(&javRec).Error; err != nil {
		t.Fatalf("create jav: %v", err)
	}

	initial, err := UpsertVideoLocation(ctx, oldVideo.ID, dir.ID, "same/path.mp4", now)
	if err != nil {
		t.Fatalf("upsert initial location: %v", err)
	}
	if err := gdb.Model(&models.VideoLocation{}).
		Where("id = ?", initial.ID).
		Update("jav_id", javRec.ID).Error; err != nil {
		t.Fatalf("link initial location to jav: %v", err)
	}

	sameVideo, err := UpsertVideoLocation(ctx, oldVideo.ID, dir.ID, "same/path.mp4", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("upsert unchanged video location: %v", err)
	}
	if sameVideo.ID != initial.ID {
		t.Fatalf("unchanged path created a new location: got id %d, want %d", sameVideo.ID, initial.ID)
	}
	if sameVideo.JavID == nil || *sameVideo.JavID != javRec.ID {
		t.Fatalf("unchanged video should retain jav link: %#v", sameVideo.JavID)
	}

	replaced, err := UpsertVideoLocation(ctx, newVideo.ID, dir.ID, "same/path.mp4", now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("upsert replacement video location: %v", err)
	}
	if replaced.ID != initial.ID {
		t.Fatalf("replacement path created a new location: got id %d, want %d", replaced.ID, initial.ID)
	}
	if replaced.VideoID != newVideo.ID {
		t.Fatalf("replacement video id = %d, want %d", replaced.VideoID, newVideo.ID)
	}
	if replaced.JavID != nil {
		t.Fatalf("replacement video retained stale jav link: %#v", replaced.JavID)
	}

	var reloaded models.VideoLocation
	if err := gdb.First(&reloaded, initial.ID).Error; err != nil {
		t.Fatalf("reload replacement location: %v", err)
	}
	if reloaded.VideoID != newVideo.ID || reloaded.JavID != nil {
		t.Fatalf("unexpected persisted replacement location: %#v", reloaded)
	}
}
