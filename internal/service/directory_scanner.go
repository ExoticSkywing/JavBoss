package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"javboss/internal/common"
	"javboss/internal/common/logging"
	"javboss/internal/db"
	"javboss/internal/models"
)

var (
	ErrDirectoryScanInProgress = errors.New("directory scan in progress")
	dirScanMu                  sync.Mutex
	dirScanActive              = map[int64]*directoryScanSession{}
	javLibraryScanRunning      atomic.Bool
)

// directoryScanSession 表示一个正在运行或被目录更新操作暂时占用的扫描会话。
type directoryScanSession struct {
	cancel  context.CancelFunc
	done    chan struct{}
	reserve bool
}

const (
	DirectoryWorkIdle                  = "idle"
	DirectoryWorkScanning              = "scanning"
	DirectoryWorkOrganizing            = "organizing"
	DirectoryWorkGeneratingSidecar     = "generating_sidecar"
	DirectoryWorkOrganizingWithSidecar = "organizing_with_sidecar"
	DirectoryWorkRescanning            = "rescanning"
)

// IsDirectoryScanning 判断指定目录是否正在执行文件扫描；目录更新使用的临时占用不算扫描中。
func IsDirectoryScanning(id int64) bool {
	if id <= 0 {
		return false
	}

	dirScanMu.Lock()
	defer dirScanMu.Unlock()
	session := dirScanActive[id]
	return session != nil && !session.reserve
}

// DirectoryWorkStatus 返回目录当前的处理任务、扫描任务或空闲状态。
func DirectoryWorkStatus(id int64) string {
	if status := activeDirectoryProcessingStatus(id); status != "" {
		return status
	}
	if IsDirectoryScanning(id) {
		return DirectoryWorkScanning
	}
	return DirectoryWorkIdle
}

// acquireDirectoryScanSession 获取单目录扫描会话，保证同一目录同一时间只运行一个扫描任务。
func acquireDirectoryScanSession(ctx context.Context, id int64) (context.Context, func(), error) {
	if id <= 0 {
		return nil, nil, errors.New("directory id cannot be zero")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	dirScanMu.Lock()
	defer dirScanMu.Unlock()
	if _, ok := dirScanActive[id]; ok {
		return nil, nil, ErrDirectoryScanInProgress
	}

	scanCtx, cancel := context.WithCancel(ctx)
	session := &directoryScanSession{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	dirScanActive[id] = session

	var finishOnce sync.Once
	finish := func() {
		finishOnce.Do(func() {
			cancel()
			dirScanMu.Lock()
			if dirScanActive[id] == session {
				delete(dirScanActive, id)
			}
			close(session.done)
			dirScanMu.Unlock()
		})
	}
	return scanCtx, finish, nil
}

// CancelAndReserveDirectoryScan 取消并等待指定目录的活动扫描结束，然后占用扫描会话；
// 调用返回的 release 函数后，其他扫描任务才可再次进入。
func CancelAndReserveDirectoryScan(ctx context.Context, id int64) (func(), error) {
	if id <= 0 {
		return nil, errors.New("directory id cannot be zero")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	for {
		dirScanMu.Lock()
		session := dirScanActive[id]
		if session == nil {
			reservation := &directoryScanSession{done: make(chan struct{}), reserve: true}
			dirScanActive[id] = reservation
			dirScanMu.Unlock()
			var releaseOnce sync.Once
			return func() {
				releaseOnce.Do(func() {
					dirScanMu.Lock()
					if dirScanActive[id] == reservation {
						delete(dirScanActive, id)
					}
					close(reservation.done)
					dirScanMu.Unlock()
				})
			}, nil
		}
		if session.reserve {
			dirScanMu.Unlock()
			return nil, ErrDirectoryScanInProgress
		}
		session.cancel()
		done := session.done
		dirScanMu.Unlock()

		select {
		case <-done:
		case <-ctx.Done():
			return nil, fmt.Errorf("cancel directory scan: %w", ctx.Err())
		}
	}
}

// isAutomaticDirectoryScanDue 根据目录的最新开关、周期和上次完成时间判断是否到期。
func isAutomaticDirectoryScanDue(directory models.Directory, now time.Time) bool {
	if directory.ID <= 0 || directory.IsDelete || !directory.AutoScanEnabled {
		return false
	}
	intervalMinutes := directory.AutoScanIntervalMinutes
	if intervalMinutes <= 0 {
		intervalMinutes = 1
	}
	finishedAt := directory.LastScanSummary.FinishedAtUnixMS
	if finishedAt <= 0 {
		return true
	}
	return !now.Before(time.UnixMilli(finishedAt).Add(time.Duration(intervalMinutes) * time.Minute))
}

// dispatchDueAutomaticDirectoryScans 为每个已到期目录启动独立扫描任务并立即返回。
// 同一目录在 activeScans 中只允许存在一个任务，任务开始前还会重新读取最新目录设置。
func dispatchDueAutomaticDirectoryScans(
	ctx context.Context,
	now time.Time,
	activeScans *sync.Map,
	runningScans *sync.WaitGroup,
) error {
	if common.DB == nil {
		return errors.New("nil db")
	}
	dirs, err := db.ListActiveDirectories(ctx)
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		if !isAutomaticDirectoryScanDue(dir, now) {
			continue
		}
		if _, alreadyActive := activeScans.LoadOrStore(dir.ID, struct{}{}); alreadyActive {
			continue
		}
		runningScans.Add(1)
		go func(id int64, queuedAt time.Time) {
			defer runningScans.Done()
			defer activeScans.Delete(id)

			current, err := loadDirectoryIfAutomaticScanDue(ctx, id, queuedAt)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					logging.Error("reload automatic scan directory failed id=%d err=%v", id, err)
				}
				return
			}
			if current == nil {
				return
			}
			if _, err := ScanDirectory(ctx, *current); err != nil {
				if !errors.Is(err, ErrDirectoryScanInProgress) && !errors.Is(err, context.Canceled) {
					logging.Error("automatic directory scan failed id=%d path=%s err=%v", current.ID, current.Path, err)
				}
				return
			}
			if err := enqueueMissingCoversForDirectory(ctx, current.ID); err != nil && !errors.Is(err, context.Canceled) {
				logging.Error("jav cover enqueue after automatic scan failed id=%d err=%v", current.ID, err)
			}
		}(dir.ID, now)
	}
	return nil
}

// loadDirectoryIfAutomaticScanDue 重新读取排队目录；若目录已删除、已关闭或尚未到期则返回 nil。
func loadDirectoryIfAutomaticScanDue(ctx context.Context, id int64, notBefore time.Time) (*models.Directory, error) {
	dir, err := db.GetDirectory(ctx, id)
	if err != nil {
		return nil, err
	}
	if dir == nil {
		return nil, nil
	}
	checkAt := time.Now()
	if checkAt.Before(notBefore) {
		checkAt = notBefore
	}
	if !isAutomaticDirectoryScanDue(*dir, checkAt) {
		return nil, nil
	}
	return dir, nil
}

// StartAutomaticDirectoryScanScheduler 启动自动扫描调度器；立即检查一次，之后按轮询周期检查到期目录。
func StartAutomaticDirectoryScanScheduler(ctx context.Context, pollInterval time.Duration) {
	go func() {
		var activeScans sync.Map
		var runningScans sync.WaitGroup
		defer runningScans.Wait()

		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			if err := dispatchDueAutomaticDirectoryScans(
				ctx,
				time.Now(),
				&activeScans,
				&runningScans,
			); err != nil {
				logging.Error("periodic directory scan failed: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// RequestJavLibraryIncrementalScan schedules one scan of the configured local
// library after a top-down acceptance moves files into the formal CloudNAS
// directory. It deliberately bypasses AutoScanEnabled: accepting a download
// is an explicit request to make that work discoverable immediately, while the
// normal periodic scanner setting may remain disabled.
func RequestJavLibraryIncrementalScan() {
	if !javLibraryScanRunning.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer javLibraryScanRunning.Store(false)
		// CloudNAS can expose a completed MoveFile a moment after the gRPC
		// response. A short grace period avoids racing the mount refresh.
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		<-timer.C

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		directories, err := db.ListActiveDirectories(ctx)
		if err != nil {
			logging.Error("list directories for accepted JAV incremental scan failed: %v", err)
			return
		}
		directories = selectJavLibraryDirectories(directories, os.Getenv("JAV_LIBRARY_PATH"))
		for _, directory := range directories {
			if _, err := ScanDirectory(ctx, directory); err != nil {
				if !errors.Is(err, ErrDirectoryScanInProgress) && !errors.Is(err, context.Canceled) {
					logging.Error("accepted JAV incremental scan failed id=%d path=%s err=%v", directory.ID, directory.Path, err)
				}
				continue
			}
			logging.Info("accepted JAV incremental scan completed id=%d path=%s", directory.ID, directory.Path)
		}
	}()
}

func selectJavLibraryDirectories(directories []models.Directory, configuredLibraryPath string) []models.Directory {
	if len(directories) <= 1 {
		return directories
	}
	base := path.Base(strings.TrimRight(strings.TrimSpace(filepath.ToSlash(configuredLibraryPath)), "/"))
	if base == "" || base == "." || base == "/" {
		return directories
	}
	matched := make([]models.Directory, 0, len(directories))
	for _, directory := range directories {
		if path.Base(filepath.ToSlash(filepath.Clean(directory.Path))) == base {
			matched = append(matched, directory)
		}
	}
	if len(matched) > 0 {
		return matched
	}
	// With multiple unrelated directories, do not launch a broad scan when the
	// CloudDrive library cannot be mapped unambiguously.
	return nil
}
