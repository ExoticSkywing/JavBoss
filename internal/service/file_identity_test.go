package service

import (
	"os"
	"strings"
	"testing"
	"time"

	"javboss/internal/models"
)

func TestCanReuseScannedLocationRequiresPersistedIdentityWhenAvailable(t *testing.T) {
	modified := time.Unix(1710000000, 0).UTC()
	video := &models.Video{Size: 1024}
	location := &models.VideoLocation{ModifiedAt: modified, FileIdentity: "dev=1,ino=2,ctime=3"}

	if !canReuseScannedLocation(location, video, 1024, modified, "dev=1,ino=2,ctime=3") {
		t.Fatal("matching stat identity should reuse the location")
	}
	if canReuseScannedLocation(location, video, 1024, modified, "dev=1,ino=9,ctime=4") {
		t.Fatal("changed stat identity should force a reprobe")
	}
	if canReuseScannedLocation(&models.VideoLocation{ModifiedAt: modified}, video, 1024, modified, "dev=1,ino=2,ctime=3") {
		t.Fatal("legacy location without identity should be probed once")
	}
	if !canReuseScannedLocation(&models.VideoLocation{ModifiedAt: modified}, video, 1024, modified, "") {
		t.Fatal("unsupported stat identity should retain size/mtime fallback")
	}
}

func TestFilesystemIdentityIsStableAndExcludesAccessTime(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "identity-")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}
	info, err := os.Stat(file.Name())
	if err != nil {
		t.Fatalf("stat temp file: %v", err)
	}
	first := filesystemIdentity(info)
	second := filesystemIdentity(info)
	if first == "" {
		t.Skip("platform does not expose a stable file identity")
	}
	if first != second {
		t.Fatalf("filesystem identity changed without a file change: %q vs %q", first, second)
	}
	if strings.Contains(first, "Atim") {
		t.Fatalf("filesystem identity must not include access time: %q", first)
	}
}
