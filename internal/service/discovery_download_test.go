package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"javboss/internal/downloader"
)

func TestParseMagnetInfoHash(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "hex", value: "magnet:?xt=urn:btih:0123456789ABCDEF0123456789ABCDEF01234567&dn=test", want: "0123456789abcdef0123456789abcdef01234567"},
		{name: "base32", value: "magnet:?xt=urn:btih:ABCDEFGHIJKLMNOPQRSTUVWXYZ234567", want: "abcdefghijklmnopqrstuvwxyz234567"},
		{name: "missing hash", value: "magnet:?dn=test", wantErr: true},
		{name: "wrong scheme", value: "https://example.com/file", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseMagnetInfoHash(test.value)
			if test.wantErr {
				if err == nil {
					t.Fatalf("ParseMagnetInfoHash() = %q, want error", got)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("ParseMagnetInfoHash() = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestFilterRemoteVideosExcludesSamples(t *testing.T) {
	files := []downloader.RemoteFile{
		{Name: "ABC-001.mp4"},
		{Name: "ABC-001-CD2.mkv"},
		{Name: "ABC-001-sample.mp4"},
		{Name: "poster.jpg"},
	}
	got := filterRemoteVideos(files)
	if len(got) != 2 || got[0].Name != "ABC-001.mp4" || got[1].Name != "ABC-001-CD2.mkv" {
		t.Fatalf("filterRemoteVideos() = %#v", got)
	}
}

func TestSafeLocalDownloadPathStaysInsideRoot(t *testing.T) {
	root := t.TempDir()
	got, err := safeLocalDownloadPath(root, "../../folder/ABC-001.mp4")
	if err != nil {
		t.Fatalf("safeLocalDownloadPath() error = %v", err)
	}
	want := filepath.Join(root, "folder", "ABC-001.mp4")
	if got != want {
		t.Fatalf("safeLocalDownloadPath() = %q, want %q", got, want)
	}
}

func TestSafeLocalNamePreservesUnicodeAndExtension(t *testing.T) {
	if got := safeLocalName("作品：ABC-001.mp4"); got != "作品：ABC-001.mp4" {
		t.Fatalf("safeLocalName() = %q", got)
	}
}

func TestLocalDownloadLimiterCanIncreaseWhileJobsWait(t *testing.T) {
	limiter := newLocalDownloadLimiter(1)
	if err := limiter.acquire(context.Background()); err != nil {
		t.Fatalf("acquire first slot: %v", err)
	}

	acquired := make(chan error, 1)
	go func() {
		acquired <- limiter.acquire(context.Background())
	}()
	select {
	case err := <-acquired:
		t.Fatalf("second slot acquired before limit changed: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	limiter.setLimit(2)
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatalf("acquire second slot: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second slot did not acquire after limit increased")
	}
	limiter.release()
	limiter.release()
}

func TestLocalDownloadLimiterWaitHonorsCancellation(t *testing.T) {
	limiter := newLocalDownloadLimiter(1)
	if err := limiter.acquire(context.Background()); err != nil {
		t.Fatalf("acquire first slot: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := limiter.acquire(ctx); err != context.Canceled {
		t.Fatalf("waiting acquire error = %v, want context.Canceled", err)
	}
	limiter.release()
}
