package service

import (
	"path/filepath"
	"testing"

	cloudpb "javboss/internal/clouddrive/proto"
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

func TestFilterCloudDriveVideosExcludesSamples(t *testing.T) {
	files := []*cloudpb.CloudDriveFile{
		{Name: "ABC-001.mp4"},
		{Name: "ABC-001-CD2.mkv"},
		{Name: "ABC-001-sample.mp4"},
		{Name: "poster.jpg"},
	}
	got := filterCloudDriveVideos(files)
	if len(got) != 2 || got[0].GetName() != "ABC-001.mp4" || got[1].GetName() != "ABC-001-CD2.mkv" {
		t.Fatalf("filterCloudDriveVideos() = %#v", got)
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
