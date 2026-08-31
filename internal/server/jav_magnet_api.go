package server

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"javboss/internal/db"
	"javboss/internal/jav"
	"javboss/internal/models"
	"javboss/internal/util"
)

type javMagnetReviewRequest struct {
	QualityClear   *bool    `json:"quality_clear"`
	Confirmed1080P *bool    `json:"confirmed_1080p"`
	HasIntroAd     *bool    `json:"has_intro_ad"`
	HasWatermark   *bool    `json:"has_watermark"`
	HasMarquee     *bool    `json:"has_marquee"`
	IsUncensored   *bool    `json:"is_uncensored"`
	Reasons        []string `json:"reasons"`
	Notes          string   `json:"notes"`
	Accepted       bool     `json:"accepted"`
	DeleteFile     *bool    `json:"delete_file"`
}

type javMagnetSelectionRequest struct {
	CandidateID int64 `json:"candidate_id"`
}

type javDownloadBatchRequest struct {
	JavIDs []int64 `json:"jav_ids"`
}

type javDownloadAttemptUpdateRequest struct {
	Status         string `json:"status"`
	ExternalTaskID string `json:"external_task_id"`
	Error          string
}

func registerJavDownloadCallbackRoutes(router gin.IRoutes) {
	router.PATCH("/jav/magnet-queue/attempts/:attempt_id", requireJavDownloadCallbackToken(), updateJavDownloadAttempt)
}

func requireJavDownloadCallbackToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		expected := strings.TrimSpace(os.Getenv("JAVBOSS_CLOUD_DOWNLOAD_CALLBACK_TOKEN"))
		if expected == "" {
			abortLocalizedError(c, http.StatusServiceUnavailable, "尚未配置云下载回调密钥", "Cloud download callback token is not configured")
			return
		}
		provided := ""
		parts := strings.Fields(c.GetHeader("Authorization"))
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			provided = parts[1]
		}
		if len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			abortLocalizedError(c, http.StatusUnauthorized, "云下载回调密钥无效", "Invalid cloud download callback token")
			return
		}
		c.Next()
	}
}

// collectJavMagnets fetches JavDB candidates and persists them on the
// canonical work. Re-fetching is idempotent and never erases review history.
func collectJavMagnets(c *gin.Context) {
	id, ok := parsePositiveID(c.Param("id"), c, "JAV 作品 ID 无效", "Invalid JAV item ID")
	if !ok {
		return
	}
	item, err := db.GetJav(c.Request.Context(), id, parseDirectoryIDs(c.Query("directory_ids")))
	if err != nil {
		writeJavMagnetError(c, err, "读取 JAV 作品失败", "Failed to load JAV item")
		return
	}
	response := jav.DefaultJavDBAppClient().ResolveBatch(c.Request.Context(), []string{item.Code})
	if len(response.Items) == 0 {
		respondLocalizedError(c, http.StatusBadGateway, "JavDB 未返回磁链结果", "JavDB returned no magnet result")
		return
	}
	resolved := response.Items[0]
	if resolved.Error != "" {
		respondLocalizedError(c, http.StatusBadGateway, resolved.Error, "JavDB magnet lookup failed")
		return
	}
	candidates, err := db.UpsertJavMagnetCandidates(c.Request.Context(), id, resolved.Magnets)
	if err != nil {
		writeJavMagnetError(c, err, "保存磁链候选失败", "Failed to save magnet candidates")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"movie": resolved.Movie, "candidates": candidates})
}

func selectJavMagnet(c *gin.Context) {
	id, ok := parsePositiveID(c.Param("id"), c, "JAV 作品 ID 无效", "Invalid JAV item ID")
	if !ok {
		return
	}
	var request javMagnetSelectionRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.CandidateID <= 0 {
		respondLocalizedError(c, http.StatusBadRequest, "磁链候选 ID 无效", "Invalid magnet candidate ID")
		return
	}
	selection, err := db.SelectJavMagnet(c.Request.Context(), id, request.CandidateID)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, db.ErrJavMagnetNotFound) {
			status = http.StatusNotFound
		}
		if errors.Is(err, db.ErrJavMagnetAlreadyRejected) || errors.Is(err, db.ErrJavDownloadAlreadyActive) || errors.Is(err, db.ErrJavAlreadyQualityAccepted) {
			status = http.StatusConflict
		}
		writeJavMagnetErrorStatus(c, status, err, "保存磁链选择失败", "Failed to save magnet selection")
		return
	}
	c.JSON(http.StatusOK, selection)
}

func listJavDownloadQueue(c *gin.Context) {
	items, total, err := db.ListJavDownloadQueue(c.Request.Context(), positiveIntQuery(c.Query("page_size"), 50), queryInt(c, "offset", 0))
	if err != nil {
		writeJavMagnetError(c, err, "读取待发送磁链失败", "Failed to load magnet send queue")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total})
}

func listJavImportDays(c *gin.Context) {
	items, total, err := db.ListJavImportDaySummaries(c.Request.Context(), positiveIntQuery(c.Query("page_size"), 31), queryInt(c, "offset", 0), parseDirectoryIDs(c.Query("directory_ids")))
	if err != nil {
		writeJavMagnetError(c, err, "读取入库记录失败", "Failed to load import history")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total})
}

func listJavQualityReviewQueue(c *gin.Context) {
	items, total, err := db.ListJavQualityReviewQueue(c.Request.Context(), positiveIntQuery(c.Query("page_size"), 50), queryInt(c, "offset", 0), parseDirectoryIDs(c.Query("directory_ids")))
	if err != nil {
		writeJavMagnetError(c, err, "读取待验收作品失败", "Failed to load quality review queue")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total})
}

func submitJavDownloadBatch(c *gin.Context) {
	var request javDownloadBatchRequest
	if err := c.ShouldBindJSON(&request); err != nil || len(request.JavIDs) == 0 {
		respondLocalizedError(c, http.StatusBadRequest, "至少选择一个 JAV 作品", "Select at least one JAV item")
		return
	}
	submitter, configured, configErr := configuredJavDownloadSubmitter()
	if configErr != nil {
		respondLocalizedError(c, http.StatusInternalServerError, "云下载服务地址配置无效："+configErr.Error(), "Cloud download service URL is invalid: "+configErr.Error())
		return
	}
	if !configured {
		respondLocalizedError(c, http.StatusServiceUnavailable, "尚未配置云下载服务；磁链选择仍保存在待发送队列", "Cloud download service is not configured; the magnet selection remains in the send queue")
		return
	}
	batch, err := db.CreateJavDownloadBatch(c.Request.Context(), request.JavIDs)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, db.ErrJavMagnetNoSelection) {
			status = http.StatusBadRequest
		}
		if errors.Is(err, db.ErrJavMagnetAlreadyRejected) || errors.Is(err, db.ErrJavDownloadAlreadyActive) || errors.Is(err, db.ErrJavAlreadyHasFile) || errors.Is(err, db.ErrJavAlreadyQualityAccepted) {
			status = http.StatusConflict
		}
		writeJavMagnetErrorStatus(c, status, err, "创建磁链发送批次失败", "Failed to create magnet send batch")
		return
	}
	items, err := db.ListJavDownloadSubmission(c.Request.Context(), batch.ID)
	if err != nil {
		_ = failJavDownloadBatch(c, batch, err, models.JavDownloadAttemptFailed)
		writeJavMagnetError(c, err, "读取磁链发送内容失败", "Failed to load magnet submission payload")
		return
	}
	result, err := submitter.Submit(c.Request.Context(), batch.ID, items)
	if err != nil {
		_ = failJavDownloadBatch(c, batch, err, models.JavDownloadAttemptUncertain)
		respondLocalizedError(c, http.StatusBadGateway, "云下载服务发送失败："+err.Error(), "Cloud download submission failed: "+err.Error())
		return
	}
	for _, item := range items {
		task := findJavSubmissionTask(result.Tasks, item)
		status := normalizeExternalJavDownloadStatus(task.Status)
		if _, err := db.MarkJavDownloadAttempt(c.Request.Context(), item.AttemptID, status, task.ExternalTaskID, task.Error); err != nil {
			writeJavMagnetError(c, err, "保存云下载任务状态失败", "Failed to save cloud download task status")
			return
		}
	}
	finalBatch, err := db.MarkJavDownloadBatchExternal(c.Request.Context(), batch.ID, result.ExternalBatchID)
	if err != nil {
		writeJavMagnetError(c, err, "保存云下载批次状态失败", "Failed to save cloud download batch status")
		return
	}
	deliveryStatus := "submitted"
	if finalBatch.Status == models.JavDownloadBatchPartial {
		deliveryStatus = "partial"
	}
	if finalBatch.Status == models.JavDownloadBatchFailed {
		deliveryStatus = "failed"
	}
	c.JSON(http.StatusCreated, gin.H{"batch": finalBatch, "delivery_status": deliveryStatus})
}

func findJavSubmissionTask(tasks []javDownloadSubmissionTask, item db.JavDownloadSubmissionItem) javDownloadSubmissionTask {
	for _, task := range tasks {
		if task.AttemptID == item.AttemptID || (task.AttemptID == 0 && strings.TrimSpace(task.IdempotencyKey) == item.IdempotencyKey) {
			return task
		}
	}
	return javDownloadSubmissionTask{Status: "submitted"}
}

func normalizeExternalJavDownloadStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "downloaded":
		return "downloaded"
	case "awaiting_quality", "quality_review":
		return "awaiting_quality"
	case "failed":
		return "failed"
	case "rejected":
		// A downloader may reject a hand-off, but it cannot make JavBoss's
		// human quality decision. Treat this as a transport failure so the
		// candidate itself remains available for a deliberate review.
		return "failed"
	default:
		// queued/accepted/submitted from a transport service all mean that the
		// hand-off succeeded; quality acceptance is a separate user decision.
		return "submitted"
	}
}

func failJavDownloadBatch(c *gin.Context, batch *models.JavDownloadBatch, err error, status string) error {
	if batch == nil {
		return err
	}
	items, loadErr := db.ListJavDownloadSubmission(c.Request.Context(), batch.ID)
	if loadErr != nil {
		return loadErr
	}
	for _, item := range items {
		if _, markErr := db.MarkJavDownloadAttempt(c.Request.Context(), item.AttemptID, status, "", err.Error()); markErr != nil {
			return markErr
		}
	}
	return nil
}

func getJavDownloadBatch(c *gin.Context) {
	id, ok := parsePositiveID(c.Param("batch_id"), c, "发送批次 ID 无效", "Invalid download batch ID")
	if !ok {
		return
	}
	batch, err := db.GetJavDownloadBatch(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondLocalizedError(c, http.StatusNotFound, "发送批次不存在", "Download batch was not found")
		} else {
			writeJavMagnetError(c, err, "读取发送批次失败", "Failed to load download batch")
		}
		return
	}
	c.JSON(http.StatusOK, batch)
}

func updateJavDownloadAttempt(c *gin.Context) {
	id, ok := parsePositiveID(c.Param("attempt_id"), c, "下载尝试 ID 无效", "Invalid download attempt ID")
	if !ok {
		return
	}
	var request javDownloadAttemptUpdateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondLocalizedError(c, http.StatusBadRequest, "下载状态请求无效", "Invalid download status request")
		return
	}
	status := strings.ToLower(strings.TrimSpace(request.Status))
	if status != "submitted" && status != "downloaded" && status != "awaiting_quality" && status != "failed" {
		respondLocalizedError(c, http.StatusBadRequest, "外部服务只能更新提交、下载、待验收或失败状态", "External services may only report submitted, downloaded, awaiting_quality, or failed")
		return
	}
	attempt, err := db.MarkJavDownloadAttempt(c.Request.Context(), id, status, request.ExternalTaskID, request.Error)
	if err != nil {
		writeJavMagnetError(c, err, "更新下载状态失败", "Failed to update download status")
		return
	}
	c.JSON(http.StatusOK, attempt)
}

func reviewJavMagnet(c *gin.Context) {
	javID, ok := parsePositiveID(c.Param("id"), c, "JAV 作品 ID 无效", "Invalid JAV item ID")
	if !ok {
		return
	}
	candidateID, ok := parsePositiveID(c.Param("candidate_id"), c, "磁链候选 ID 无效", "Invalid magnet candidate ID")
	if !ok {
		return
	}
	var request javMagnetReviewRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondLocalizedError(c, http.StatusBadRequest, "磁链验收请求无效", "Invalid magnet review request")
		return
	}
	if request.Accepted {
		acceptance, err := db.AcceptJavDownloadedWorkWithReview(c.Request.Context(), javID, candidateID, 0, db.JavMagnetReviewInput{
			QualityClear: request.QualityClear, Confirmed1080P: request.Confirmed1080P,
			HasIntroAd: request.HasIntroAd, HasWatermark: request.HasWatermark,
			HasMarquee: request.HasMarquee, IsUncensored: request.IsUncensored,
			Reasons: request.Reasons, Notes: request.Notes, Accepted: true,
		})
		if err != nil {
			writeJavReviewError(c, err, "确认作品入库失败", "Failed to accept downloaded work")
			return
		}
		c.JSON(http.StatusOK, acceptance)
		return
	}
	candidate, err := db.RejectJavDownloadedWorkWithReview(c.Request.Context(), javID, candidateID, 0, db.JavMagnetReviewInput{
		QualityClear: request.QualityClear, Confirmed1080P: request.Confirmed1080P,
		HasIntroAd: request.HasIntroAd, HasWatermark: request.HasWatermark,
		HasMarquee: request.HasMarquee, IsUncensored: request.IsUncensored,
		Reasons: request.Reasons, Notes: request.Notes,
	})
	if err != nil {
		writeJavReviewError(c, err, "保存磁链不合格结论失败", "Failed to save rejected magnet review")
		return
	}
	deleteFile := true
	if request.DeleteFile != nil {
		deleteFile = *request.DeleteFile
	}
	if deleteFile {
		if err := deleteLatestJavReviewLocation(c, javID); err != nil {
			writeJavMagnetError(c, err, "磁链已标记不合格，但删除落盘文件失败", "Magnet was rejected, but deleting the downloaded file failed")
			return
		}
	}
	c.JSON(http.StatusOK, candidate)
}

func writeJavReviewError(c *gin.Context, err error, zh, en string) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, db.ErrJavMagnetNotFound):
		status = http.StatusNotFound
	case errors.Is(err, db.ErrJavDownloadNoFile),
		errors.Is(err, db.ErrJavDownloadAttemptRequired),
		errors.Is(err, db.ErrJavDownloadAmbiguousFile),
		errors.Is(err, db.ErrJavMagnetAlreadyRejected),
		errors.Is(err, db.ErrJavDownloadCandidateMismatch),
		errors.Is(err, db.ErrJavAlreadyQualityAccepted):
		status = http.StatusConflict
	}
	writeJavMagnetErrorStatus(c, status, err, zh, en)
}

func deleteLatestJavReviewLocation(c *gin.Context, javID int64) error {
	item, err := db.GetJav(c.Request.Context(), javID, nil)
	if err != nil {
		return fmt.Errorf("load downloaded JAV locations: %w", err)
	}
	if len(item.Videos) == 0 {
		return nil
	}
	if len(item.Videos) != 1 {
		return fmt.Errorf("refuse to guess among %d active file locations", len(item.Videos))
	}
	latest := item.Videos[0]
	if latest.LocationID <= 0 || latest.Path == "" || latest.DirectoryRef.Path == "" {
		return nil
	}
	if latest.DirectoryRef.Missing {
		return errors.New("refuse to remove a file while its storage directory is offline")
	}
	fullPath, _, err := resolveVideoPath(latest.Path, latest.DirectoryRef.Path)
	if err != nil {
		return fmt.Errorf("resolve downloaded JAV path: %w", err)
	}
	if err := util.MoveFileToTrash(fullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("move rejected JAV file to trash: %w", err)
	}
	if err := db.HideVideoLocationsByIDs(c.Request.Context(), []int64{latest.LocationID}); err != nil {
		return fmt.Errorf("hide rejected JAV location: %w", err)
	}
	return nil
}

func writeJavMagnetError(c *gin.Context, err error, zh, en string) {
	writeJavMagnetErrorStatus(c, http.StatusInternalServerError, err, zh, en)
}
func writeJavMagnetErrorStatus(c *gin.Context, status int, err error, zh, en string) {
	if err == nil {
		err = errors.New(zh)
	}
	respondLocalizedError(c, status, zh+"："+err.Error(), en)
}

func parsePositiveID(raw string, c *gin.Context, zh, en string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		respondLocalizedError(c, http.StatusBadRequest, zh, en)
		return 0, false
	}
	return id, true
}
