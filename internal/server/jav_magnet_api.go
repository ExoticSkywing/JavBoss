package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"javboss/internal/common/logging"
	"javboss/internal/db"
	"javboss/internal/jav"
	"javboss/internal/models"
	"javboss/internal/service"
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

type javQualityReviewBatchRequest struct {
	AttemptIDs []int64 `json:"attempt_ids"`
}

type javMagnetSelectionRequest struct {
	CandidateID int64 `json:"candidate_id"`
}

type javDownloadBatchRequest struct {
	JavIDs []int64 `json:"jav_ids"`
}

type javDownloadAttemptUpdateRequest struct {
	Status         string   `json:"status"`
	ExternalTaskID string   `json:"external_task_id"`
	Error          string   `json:"error"`
	ResultPaths    []string `json:"result_paths"`
}

func registerJavDownloadCallbackRoutes(router gin.IRoutes) {
	router.PATCH(
		"/jav/magnet-queue/attempts/:attempt_id",
		requireBearerEnv("JAVBOSS_CLOUD_DOWNLOAD_CALLBACK_TOKEN", "云下载回调密钥"),
		updateJavDownloadAttempt,
	)
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
	if resolved.Movie == nil || models.NormalizeJavCode(resolved.Movie.Number) != models.NormalizeJavCode(item.Code) {
		respondLocalizedError(c, http.StatusBadGateway, "JavDB 返回了不匹配的作品", "JavDB returned a mismatched work")
		return
	}
	candidates, err := db.UpsertJavMagnetCandidatesForCode(c.Request.Context(), id, item.Code, resolved.Magnets)
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

func listJavMagnetSamples(c *gin.Context) {
	status := strings.ToLower(strings.TrimSpace(c.Query("status")))
	if status != "" && status != "all" && status != models.JavMagnetReviewAccepted && status != models.JavMagnetReviewRejected {
		respondLocalizedError(c, http.StatusBadRequest, "磁链样本状态无效", "Invalid magnet sample status")
		return
	}
	items, total, stats, err := db.ListJavMagnetSamples(
		c.Request.Context(),
		status,
		c.Query("search"),
		c.Query("sort"),
		c.Query("direction"),
		positiveIntQuery(c.Query("page_size"), 40),
		queryInt(c, "offset", 0),
	)
	if err != nil {
		writeJavMagnetError(c, err, "读取磁链样本失败", "Failed to load magnet samples")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "stats": stats})
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

// saveJavQualityReviewDecision records the human decision only. Files stay in
// the CloudDrive2 staging directory until the queue's batch action is run.
func saveJavQualityReviewDecision(c *gin.Context) {
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
	attempt, err := db.GetJavDownloadAttemptForReview(c.Request.Context(), javID, candidateID)
	if err != nil {
		writeJavReviewError(c, err, "找不到待验收下载任务", "No download attempt is awaiting quality review")
		return
	}
	saved, err := db.SaveJavQualityReviewDecision(c.Request.Context(), javID, candidateID, attempt.ID, db.JavMagnetReviewInput{
		QualityClear: request.QualityClear, Confirmed1080P: request.Confirmed1080P,
		HasIntroAd: request.HasIntroAd, HasWatermark: request.HasWatermark,
		HasMarquee: request.HasMarquee, IsUncensored: request.IsUncensored,
		Reasons: request.Reasons, Notes: request.Notes, Accepted: request.Accepted,
	})
	if err != nil {
		writeJavReviewError(c, err, "保存验收决定失败", "Failed to save quality review decision")
		return
	}
	c.JSON(http.StatusOK, saved)
}

// executeJavQualityReviewBatch performs the physical operations in as few
// CloudDrive2 calls as possible: one MoveFile for approvals and one DeleteFiles
// for rejections. Each item still receives its own durable JavBoss state.
func executeJavQualityReviewBatch(c *gin.Context) {
	var request javQualityReviewBatchRequest
	if err := c.ShouldBindJSON(&request); err != nil || len(request.AttemptIDs) == 0 {
		respondLocalizedError(c, http.StatusBadRequest, "至少选择一项已保存的验收决定", "Select at least one saved review decision")
		return
	}
	if len(request.AttemptIDs) > 100 {
		respondLocalizedError(c, http.StatusBadRequest, "单次最多执行 100 项验收", "A review batch is limited to 100 items")
		return
	}
	logging.Info("JAV quality review batch started: attempts=%v", request.AttemptIDs)
	submissions, err := db.ListJavQualityReviewSubmissions(c.Request.Context(), request.AttemptIDs)
	if err != nil {
		writeJavReviewError(c, err, "读取验收决定失败", "Failed to load saved review decisions")
		return
	}
	controller, configured, configErr := configuredJavDownloadController()
	if configErr != nil {
		respondLocalizedError(c, http.StatusInternalServerError, "云下载验收地址配置无效："+configErr.Error(), "Cloud download review URL is invalid: "+configErr.Error())
		return
	}
	if !configured {
		respondLocalizedError(c, http.StatusServiceUnavailable, "尚未配置云下载验收服务", "Cloud download review service is not configured")
		return
	}
	reviewResult, err := controller.ReviewBatch(c.Request.Context(), submissions)
	if err != nil {
		respondLocalizedError(c, http.StatusBadGateway, "云下载批量验收失败："+err.Error(), "Cloud download batch review failed: "+err.Error())
		return
	}
	byAttempt := make(map[int64]javDownloadSubmissionTask, len(reviewResult.Tasks))
	for _, result := range reviewResult.Tasks {
		byAttempt[result.AttemptID] = result
	}
	completed := make([]gin.H, 0, len(submissions))
	acceptedCount := 0
	for _, submission := range submissions {
		controlled, exists := byAttempt[submission.AttemptID]
		if !exists {
			respondLocalizedError(c, http.StatusBadGateway, fmt.Sprintf("云下载验收结果缺少任务 %d", submission.AttemptID), "Cloud download batch review omitted an attempt")
			return
		}
		status := strings.TrimSpace(controlled.Status)
		if submission.Decision == models.JavQualityReviewDecisionAccepted {
			acceptedCount++
			if status != models.JavDownloadAttemptAwaitingScan {
				respondLocalizedError(c, http.StatusBadGateway, "文件尚未成功移入正式作品库", "The file was not promoted to the formal library")
				return
			}
			if submission.Status != models.JavDownloadAttemptAwaitingScan {
				if _, err := db.ApproveJavDownloadedWorkWithReview(c.Request.Context(), submission.JavID, submission.CandidateID, submission.AttemptID, controlled.ResultPaths, db.JavMagnetReviewInput{
					QualityClear: submission.QualityClear, Confirmed1080P: submission.Confirmed1080P,
					HasIntroAd: submission.HasIntroAd, HasWatermark: submission.HasWatermark,
					HasMarquee: submission.HasMarquee, IsUncensored: submission.IsUncensored,
					Accepted: true, Notes: submission.Notes,
				}); err != nil {
					writeJavReviewError(c, err, "保存质量通过结论失败", "Failed to save the accepted quality review")
					return
				}
			}
		} else {
			if status != models.JavDownloadAttemptRejected {
				respondLocalizedError(c, http.StatusBadGateway, "暂存文件尚未成功删除", "The staged file was not deleted")
				return
			}
			if submission.Status != models.JavDownloadAttemptRejected {
				if _, err := db.RejectJavDownloadedWorkWithReview(c.Request.Context(), submission.JavID, submission.CandidateID, submission.AttemptID, db.JavMagnetReviewInput{
					QualityClear: submission.QualityClear, Confirmed1080P: submission.Confirmed1080P,
					HasIntroAd: submission.HasIntroAd, HasWatermark: submission.HasWatermark,
					HasMarquee: submission.HasMarquee, IsUncensored: submission.IsUncensored,
					Reasons: submission.Reasons, Notes: submission.Notes,
				}); err != nil {
					writeJavReviewError(c, err, "保存磁链不合格结论失败", "Failed to save rejected magnet review")
					return
				}
			}
		}
		completed = append(completed, gin.H{"attempt_id": submission.AttemptID, "jav_id": submission.JavID, "decision": submission.Decision, "status": status})
	}
	if acceptedCount > 0 {
		service.RequestJavLibraryIncrementalScan()
	}
	logging.Info("JAV quality review batch completed: attempts=%d cleanup=%v", len(completed), reviewResult.Cleanup)
	c.JSON(http.StatusOK, gin.H{"items": completed, "count": len(completed), "cleanup": reviewResult.Cleanup})
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
		if _, err := db.MarkJavDownloadAttemptWithResultPaths(c.Request.Context(), item.AttemptID, status, task.ExternalTaskID, task.Error, task.ResultPaths); err != nil {
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
	case "uncertain":
		return "uncertain"
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
	if status != "submitted" && status != "downloaded" && status != "awaiting_quality" && status != "uncertain" && status != "failed" {
		respondLocalizedError(c, http.StatusBadRequest, "外部服务只能更新提交、下载、待验收、不确定或失败状态", "External services may only report submitted, downloaded, awaiting_quality, uncertain, or failed")
		return
	}
	attempt, err := db.MarkJavDownloadAttemptWithResultPaths(c.Request.Context(), id, status, request.ExternalTaskID, request.Error, request.ResultPaths)
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
	attempt, err := db.GetJavDownloadAttemptForReview(c.Request.Context(), javID, candidateID)
	if err != nil {
		writeJavReviewError(c, err, "找不到待验收下载任务", "No download attempt is awaiting quality review")
		return
	}
	if request.Accepted && attempt.Status == models.JavDownloadAttemptAwaitingScan {
		c.JSON(http.StatusOK, attempt)
		return
	}
	if !request.Accepted && attempt.Status == models.JavDownloadAttemptRejected {
		c.JSON(http.StatusOK, attempt)
		return
	}
	if attempt.Status != models.JavDownloadAttemptAwaitingQuality {
		writeJavReviewError(c, db.ErrJavDownloadNotAwaitingQuality, "当前下载任务不在待验收状态", "The download attempt is not awaiting quality review")
		return
	}
	controller, configured, configErr := configuredJavDownloadController()
	if configErr != nil {
		respondLocalizedError(c, http.StatusInternalServerError, "云下载验收地址配置无效："+configErr.Error(), "Cloud download review URL is invalid: "+configErr.Error())
		return
	}
	if !configured {
		respondLocalizedError(c, http.StatusServiceUnavailable, "尚未配置云下载验收服务", "Cloud download review service is not configured")
		return
	}
	decision := "rejected"
	if request.Accepted {
		decision = "accepted"
	}
	controlled, err := controller.Review(c.Request.Context(), attempt.ID, decision)
	if err != nil {
		respondLocalizedError(c, http.StatusBadGateway, "云下载验收操作失败："+err.Error(), "Cloud download review operation failed: "+err.Error())
		return
	}
	if controlled.AttemptID != 0 && controlled.AttemptID != attempt.ID {
		respondLocalizedError(c, http.StatusBadGateway, "云下载验收服务返回了错误的任务 ID", "Cloud download review returned a mismatched attempt id")
		return
	}
	if request.Accepted {
		if strings.TrimSpace(controlled.Status) != models.JavDownloadAttemptAwaitingScan {
			respondLocalizedError(c, http.StatusBadGateway, "文件尚未成功移入正式作品库", "The file was not promoted to the formal library")
			return
		}
		approved, err := db.ApproveJavDownloadedWorkWithReview(c.Request.Context(), javID, candidateID, attempt.ID, controlled.ResultPaths, db.JavMagnetReviewInput{
			QualityClear: request.QualityClear, Confirmed1080P: request.Confirmed1080P,
			HasIntroAd: request.HasIntroAd, HasWatermark: request.HasWatermark,
			HasMarquee: request.HasMarquee, IsUncensored: request.IsUncensored,
			Reasons: request.Reasons, Notes: request.Notes, Accepted: true,
		})
		if err != nil {
			writeJavReviewError(c, err, "保存质量通过结论失败", "Failed to save the accepted quality review")
			return
		}
		c.JSON(http.StatusOK, approved)
		return
	}
	if strings.TrimSpace(controlled.Status) != models.JavDownloadAttemptRejected {
		respondLocalizedError(c, http.StatusBadGateway, "暂存文件尚未成功删除", "The staged file was not deleted")
		return
	}
	candidate, err := db.RejectJavDownloadedWorkWithReview(c.Request.Context(), javID, candidateID, attempt.ID, db.JavMagnetReviewInput{
		QualityClear: request.QualityClear, Confirmed1080P: request.Confirmed1080P,
		HasIntroAd: request.HasIntroAd, HasWatermark: request.HasWatermark,
		HasMarquee: request.HasMarquee, IsUncensored: request.IsUncensored,
		Reasons: request.Reasons, Notes: request.Notes,
	})
	if err != nil {
		writeJavReviewError(c, err, "保存磁链不合格结论失败", "Failed to save rejected magnet review")
		return
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
		errors.Is(err, db.ErrJavDownloadNotAwaitingQuality),
		errors.Is(err, db.ErrJavDownloadAmbiguousFile),
		errors.Is(err, db.ErrJavMagnetAlreadyRejected),
		errors.Is(err, db.ErrJavDownloadCandidateMismatch),
		errors.Is(err, db.ErrJavQualityReviewDecisionRequired),
		errors.Is(err, db.ErrJavAlreadyQualityAccepted):
		status = http.StatusConflict
	}
	writeJavMagnetErrorStatus(c, status, err, zh, en)
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
