package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"javboss/internal/clouddrive"
	cloudpb "javboss/internal/clouddrive/proto"
	"javboss/internal/common/logging"
	"javboss/internal/db"
	"javboss/internal/models"
	"javboss/internal/util"
)

const (
	cloudDrivePollInterval = 10 * time.Second
	cloudDriveRPCTimeout   = 30 * time.Second
	cloudDriveMaxWait      = 7 * 24 * time.Hour
)

var (
	cloudDriveNameUnsafe = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]+`)
	cloudDriveSampleName = regexp.MustCompile(`(?i)(^|[._ -])(sample|trailer|preview|予告|样片)([._ -]|$)`)
	cloudDriveManagerMu  sync.RWMutex
	cloudDriveManager    *downloadManager
	cloudDriveHTTPClient = newCloudDriveDownloadHTTPClient()
)

type downloadManager struct {
	ctx     context.Context
	wake    chan struct{}
	mu      sync.Mutex
	cancels map[int64]context.CancelFunc
}

func StartCloudDriveDownloadManager(ctx context.Context) {
	manager := &downloadManager{ctx: ctx, wake: make(chan struct{}, 1), cancels: make(map[int64]context.CancelFunc)}
	cloudDriveManagerMu.Lock()
	cloudDriveManager = manager
	cloudDriveManagerMu.Unlock()
	if err := db.ResetInterruptedCloudDriveDownloads(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logging.Error("reset interrupted CloudDrive2 downloads failed: %v", err)
	}
	go manager.run()
	manager.signal()
}

func WakeCloudDriveDownloadManager() {
	cloudDriveManagerMu.RLock()
	manager := cloudDriveManager
	cloudDriveManagerMu.RUnlock()
	if manager != nil {
		manager.signal()
	}
}

func CancelCloudDriveDownload(id int64) {
	cloudDriveManagerMu.RLock()
	manager := cloudDriveManager
	cloudDriveManagerMu.RUnlock()
	if manager == nil {
		return
	}
	manager.mu.Lock()
	cancel := manager.cancels[id]
	manager.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (m *downloadManager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *downloadManager) run() {
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.wake:
		}
		for {
			job, err := db.NextQueuedCloudDriveDownload(m.ctx)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					logging.Error("load CloudDrive2 download queue failed: %v", err)
				}
				break
			}
			if job == nil {
				break
			}
			jobCtx, cancel := context.WithCancel(m.ctx)
			m.mu.Lock()
			m.cancels[job.ID] = cancel
			m.mu.Unlock()
			err = processCloudDriveDownload(jobCtx, job)
			cancel()
			m.mu.Lock()
			delete(m.cancels, job.ID)
			m.mu.Unlock()
			if err != nil {
				current, loadErr := db.GetCloudDriveDownload(context.Background(), job.ID)
				if loadErr == nil && current.Status != models.CloudDriveDownloadCanceled {
					message := strings.TrimSpace(err.Error())
					if len(message) > 1000 {
						message = message[:1000]
					}
					_ = db.UpdateCloudDriveDownload(context.Background(), job.ID, map[string]any{
						"status": models.CloudDriveDownloadFailed, "error_message": message,
					})
				}
				if !errors.Is(err, context.Canceled) {
					logging.Error("CloudDrive2 download job failed id=%d: %v", job.ID, err)
				}
			}
		}
	}
}

func processCloudDriveDownload(ctx context.Context, job *models.JavDiscoveryDownload) error {
	settings, err := db.GetCloudDriveSettings(ctx)
	if err != nil {
		return err
	}
	if !settings.Enabled || settings.Address == "" || settings.APIToken == "" || settings.RemoteFolder == "" {
		return errors.New("CloudDrive2 is not fully configured")
	}
	directory, err := db.GetDirectory(ctx, job.DirectoryID)
	if err != nil {
		return err
	}
	if directory == nil || directory.IsDelete {
		return errors.New("local download directory is unavailable")
	}
	item, err := db.GetJavDiscoveryItem(ctx, job.JavDiscoveryItemID)
	if err != nil {
		return err
	}
	client, err := clouddrive.NewClient(settings.Address, settings.APIToken)
	if err != nil {
		return err
	}
	defer client.Close()

	remoteFolder := strings.TrimSpace(job.RemoteFolder)
	if remoteFolder == "" {
		name := cloudDriveJobFolderName(item.Code, job.InfoHash)
		rpcCtx, cancel := context.WithTimeout(ctx, cloudDriveRPCTimeout)
		remoteFolder, err = client.EnsureFolder(rpcCtx, settings.RemoteFolder, name)
		cancel()
		if err != nil {
			return fmt.Errorf("create CloudDrive2 job folder: %w", err)
		}
		if err := db.UpdateCloudDriveDownload(ctx, job.ID, map[string]any{"remote_folder": remoteFolder}); err != nil {
			return err
		}
	}

	if err := db.UpdateCloudDriveDownload(ctx, job.ID, map[string]any{
		"status": models.CloudDriveDownloadOfflineDownloading, "progress": 0, "error_message": "",
	}); err != nil {
		return err
	}
	found, finished, err := cloudDriveOfflineState(ctx, client, remoteFolder, job.InfoHash)
	if err != nil {
		return err
	}
	if !found {
		rpcCtx, cancel := context.WithTimeout(ctx, cloudDriveRPCTimeout)
		existingFiles, listErr := client.WalkFiles(rpcCtx, remoteFolder)
		cancel()
		if listErr == nil && len(filterCloudDriveVideos(existingFiles)) > 0 {
			finished = true
		}
	}
	if !found {
		if !finished {
			rpcCtx, cancel := context.WithTimeout(ctx, cloudDriveRPCTimeout)
			err = client.AddOffline(rpcCtx, job.MagnetURL, remoteFolder)
			cancel()
			if err != nil {
				return fmt.Errorf("submit CloudDrive2 offline download: %w", err)
			}
		}
	}
	if !finished {
		if err := waitForCloudDriveOffline(ctx, client, remoteFolder, job.InfoHash); err != nil {
			return err
		}
	}

	if err := db.UpdateCloudDriveDownload(ctx, job.ID, map[string]any{
		"status": models.CloudDriveDownloadResolvingFiles, "progress": 0,
	}); err != nil {
		return err
	}
	remoteFiles, err := waitForCloudDriveFiles(ctx, client, remoteFolder)
	if err != nil {
		return err
	}
	remoteFiles = filterCloudDriveVideos(remoteFiles)
	if len(remoteFiles) == 0 {
		return errors.New("the offline download contains no supported video files")
	}
	var total int64
	for _, file := range remoteFiles {
		if file.GetSize() > 0 {
			total += file.GetSize()
		}
	}
	if err := db.UpdateCloudDriveDownload(ctx, job.ID, map[string]any{
		"status": models.CloudDriveDownloadLocalDownloading, "bytes_total": total,
		"bytes_downloaded": 0, "progress": 0,
	}); err != nil {
		return err
	}

	localRoot := filepath.Join(directory.Path, safeLocalName(item.Code))
	if err := os.MkdirAll(localRoot, 0o755); err != nil {
		return fmt.Errorf("create local download directory: %w", err)
	}
	var completedBytes int64
	localFiles := make([]string, 0, len(remoteFiles))
	for _, remoteFile := range remoteFiles {
		remotePath := strings.TrimSpace(remoteFile.GetFullPathName())
		relative := remoteRelativePath(remoteFolder, remotePath, remoteFile.GetName())
		if remotePath == "" {
			remotePath = path.Join(remoteFolder, relative)
		}
		target, err := safeLocalDownloadPath(localRoot, relative)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create local download subdirectory: %w", err)
		}
		fileBase := completedBytes
		err = downloadCloudDriveFile(ctx, client, remoteFile, remotePath, target, func(fileDownloaded int64) error {
			downloaded := fileBase + fileDownloaded
			progress := 0.0
			if total > 0 {
				progress = float64(downloaded) * 100 / float64(total)
			}
			return db.UpdateCloudDriveDownload(ctx, job.ID, map[string]any{
				"bytes_downloaded": downloaded, "progress": progress,
			})
		})
		if err != nil {
			return err
		}
		completedBytes += max64(remoteFile.GetSize(), 0)
		localFiles = append(localFiles, target)
	}
	if err := db.CompleteCloudDriveDownload(ctx, job.ID, localFiles, total); err != nil {
		return err
	}
	if DirectoryWorkStatus(directory.ID) == DirectoryWorkIdle {
		if err := StartManualDirectoryScan(*directory); err != nil && !errors.Is(err, ErrDirectoryScanInProgress) {
			logging.Error("start scan after CloudDrive2 download failed directory=%d: %v", directory.ID, err)
		}
	}
	return nil
}

func cloudDriveOfflineState(ctx context.Context, client *clouddrive.Client, folder, infoHash string) (bool, bool, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, cloudDriveRPCTimeout)
	files, err := client.OfflineFiles(rpcCtx, folder)
	cancel()
	if err != nil {
		return false, false, err
	}
	for _, file := range files {
		candidateHash := normalizeInfoHash(file.GetInfoHash())
		if candidateHash == "" {
			if parsedHash, parseErr := ParseMagnetInfoHash(file.GetUrl()); parseErr == nil {
				candidateHash = parsedHash
			}
		}
		if candidateHash != normalizeInfoHash(infoHash) {
			continue
		}
		switch file.GetStatus() {
		case cloudpb.OfflineFileStatus_OFFLINE_FINISHED:
			return true, true, nil
		case cloudpb.OfflineFileStatus_OFFLINE_ERROR:
			return true, false, errors.New("CloudDrive2 reported an offline download error")
		default:
			return true, false, nil
		}
	}
	return false, false, nil
}

func waitForCloudDriveOffline(ctx context.Context, client *clouddrive.Client, folder, infoHash string) error {
	deadline := time.Now().Add(cloudDriveMaxWait)
	ticker := time.NewTicker(cloudDrivePollInterval)
	defer ticker.Stop()
	for {
		if time.Now().After(deadline) {
			return errors.New("CloudDrive2 offline download timed out")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			_, finished, err := cloudDriveOfflineState(ctx, client, folder, infoHash)
			if err != nil {
				return err
			}
			if finished {
				return nil
			}
		}
	}
}

func waitForCloudDriveFiles(ctx context.Context, client *clouddrive.Client, folder string) ([]*cloudpb.CloudDriveFile, error) {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		rpcCtx, cancel := context.WithTimeout(ctx, cloudDriveRPCTimeout)
		files, err := client.WalkFiles(rpcCtx, folder)
		cancel()
		if err == nil && len(files) > 0 {
			return files, nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return nil, fmt.Errorf("list CloudDrive2 offline files: %w", err)
			}
			return nil, errors.New("CloudDrive2 offline files did not appear")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func filterCloudDriveVideos(files []*cloudpb.CloudDriveFile) []*cloudpb.CloudDriveFile {
	result := make([]*cloudpb.CloudDriveFile, 0, len(files))
	for _, file := range files {
		name := strings.TrimSpace(file.GetName())
		if util.IsVideoCandidate(name) && !cloudDriveSampleName.MatchString(name) {
			result = append(result, file)
		}
	}
	return result
}

func downloadCloudDriveFile(
	ctx context.Context,
	client *clouddrive.Client,
	remote *cloudpb.CloudDriveFile,
	remotePath string,
	target string,
	progress func(int64) error,
) error {
	if existing, err := os.Stat(target); err == nil && existing.Mode().IsRegular() && existing.Size() == remote.GetSize() {
		return progress(existing.Size())
	}
	part := target + ".part"
	for attempt := 0; attempt < 4; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		rpcCtx, cancel := context.WithTimeout(ctx, cloudDriveRPCTimeout)
		source, err := client.DownloadSource(rpcCtx, remotePath)
		cancel()
		if err != nil {
			return fmt.Errorf("resolve CloudDrive2 download URL for %s: %w", remote.GetName(), err)
		}
		file, err := os.OpenFile(part, os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			return fmt.Errorf("open partial download: %w", err)
		}
		info, statErr := file.Stat()
		if statErr != nil {
			file.Close()
			return statErr
		}
		offset := info.Size()
		if remote.GetSize() > 0 && offset > remote.GetSize() {
			if err := file.Truncate(0); err != nil {
				file.Close()
				return err
			}
			offset = 0
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
		if err != nil {
			file.Close()
			return err
		}
		req.Header = source.Headers.Clone()
		if offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		}
		response, err := cloudDriveHTTPClient.Do(req)
		if err != nil {
			file.Close()
			if attempt < 3 {
				continue
			}
			return fmt.Errorf("download %s: %w", remote.GetName(), err)
		}
		if response.StatusCode == http.StatusRequestedRangeNotSatisfiable && remote.GetSize() > 0 && offset == remote.GetSize() {
			response.Body.Close()
			file.Close()
			return finalizeCloudDriveFile(part, target, remote.GetSize(), progress)
		}
		if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
			response.Body.Close()
			file.Close()
			if (response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden) && attempt < 3 {
				continue
			}
			return fmt.Errorf("download %s returned HTTP %d", remote.GetName(), response.StatusCode)
		}
		if offset > 0 && response.StatusCode == http.StatusOK {
			if err := file.Truncate(0); err != nil {
				response.Body.Close()
				file.Close()
				return err
			}
			offset = 0
		}
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			response.Body.Close()
			file.Close()
			return err
		}
		copyErr := copyCloudDriveResponse(ctx, file, response.Body, offset, progress)
		response.Body.Close()
		syncErr := file.Sync()
		closeErr := file.Close()
		if copyErr != nil {
			if attempt < 3 && !errors.Is(copyErr, context.Canceled) {
				continue
			}
			return copyErr
		}
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
		return finalizeCloudDriveFile(part, target, remote.GetSize(), progress)
	}
	return errors.New("download attempts exhausted")
}

func copyCloudDriveResponse(ctx context.Context, target *os.File, source io.Reader, offset int64, progress func(int64) error) error {
	buffer := make([]byte, 1024*1024)
	written := offset
	lastUpdate := time.Time{}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := source.Read(buffer)
		if n > 0 {
			if _, err := target.Write(buffer[:n]); err != nil {
				return err
			}
			written += int64(n)
			if time.Since(lastUpdate) >= time.Second {
				if err := progress(written); err != nil {
					return err
				}
				lastUpdate = time.Now()
			}
		}
		if errors.Is(readErr, io.EOF) {
			return progress(written)
		}
		if readErr != nil {
			return readErr
		}
	}
}

func finalizeCloudDriveFile(part, target string, expected int64, progress func(int64) error) error {
	info, err := os.Stat(part)
	if err != nil {
		return err
	}
	if expected > 0 && info.Size() != expected {
		return fmt.Errorf("downloaded size mismatch for %s: got %d, want %d", filepath.Base(target), info.Size(), expected)
	}
	if err := os.Rename(part, target); err != nil {
		return fmt.Errorf("finish local download: %w", err)
	}
	return progress(info.Size())
}

func newCloudDriveDownloadHTTPClient() *http.Client {
	client := util.NewHTTPClientWithTransport(0, func(transport *http.Transport) {
		transport.MaxIdleConns = 20
		transport.MaxIdleConnsPerHost = 4
		transport.DisableCompression = true
	})
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many download redirects")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return errors.New("unsupported download redirect")
		}
		return nil
	}
	return client
}

func ParseMagnetInfoHash(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(parsed.Scheme, "magnet") {
		return "", errors.New("invalid magnet URL")
	}
	for _, xt := range parsed.Query()["xt"] {
		if !strings.HasPrefix(strings.ToLower(xt), "urn:btih:") {
			continue
		}
		hash := normalizeInfoHash(strings.TrimSpace(xt[len("urn:btih:"):]))
		if len(hash) == 32 || len(hash) == 40 {
			for _, char := range hash {
				if !((char >= 'a' && char <= 'z') || (char >= '0' && char <= '9')) {
					return "", errors.New("invalid magnet info hash")
				}
			}
			return hash, nil
		}
	}
	return "", errors.New("magnet URL has no BTIH hash")
}

func normalizeInfoHash(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func cloudDriveJobFolderName(code, infoHash string) string {
	prefix := normalizeInfoHash(infoHash)
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	return safeLocalName(code) + "-" + prefix
}

func safeLocalName(value string) string {
	value = cloudDriveNameUnsafe.ReplaceAllString(strings.TrimSpace(value), "-")
	value = strings.Trim(value, ".-_")
	if value == "" {
		return "download"
	}
	return value
}

func remoteRelativePath(root, fullPath, fallback string) string {
	root = strings.TrimSuffix(path.Clean(root), "/")
	fullPath = path.Clean(fullPath)
	relative := strings.TrimPrefix(fullPath, root+"/")
	if relative == fullPath || relative == "." || relative == "" {
		relative = fallback
	}
	return relative
}

func safeLocalDownloadPath(root, relative string) (string, error) {
	parts := strings.FieldsFunc(strings.ReplaceAll(relative, "\\", "/"), func(char rune) bool { return char == '/' })
	cleanParts := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "." || part == ".." {
			continue
		}
		part = safeLocalName(part)
		if part != "" && part != "." && part != ".." {
			cleanParts = append(cleanParts, part)
		}
	}
	if len(cleanParts) == 0 {
		return "", errors.New("remote video has an invalid filename")
	}
	target := filepath.Join(append([]string{root}, cleanParts...)...)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("local download path escaped the target directory")
	}
	return target, nil
}

func max64(value, fallback int64) int64 {
	if value > fallback {
		return value
	}
	return fallback
}
