package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"javboss/internal/common"
	"javboss/internal/db"
	"javboss/internal/jav"
	"javboss/internal/models"

	"github.com/gin-gonic/gin"
)

func TestHTTPJavDownloadSubmitterForwardsStablePayload(t *testing.T) {
	var received javDownloadSubmitRequest
	var authorization string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method=%s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		authorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"external_batch_id":"cloud-batch-7","tasks":[{"attempt_id":11,"external_task_id":"cloud-task-11","status":"submitted"}]}`))
	}))
	defer upstream.Close()

	submitter := &httpJavDownloadSubmitter{endpoint: upstream.URL, token: "send-secret", client: &http.Client{Timeout: time.Second}}
	items := []db.JavDownloadSubmissionItem{{
		AttemptID: 11, JavID: 5, Code: "TEST-001", CandidateID: 9,
		MagnetURI: "magnet:?xt=urn:btih:test", IdempotencyKey: "jav:5:candidate:9",
	}}
	result, err := submitter.Submit(context.Background(), 7, items)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if received.BatchID != 7 || received.CallbackPath == "" || len(received.Items) != 1 || received.Items[0].IdempotencyKey != items[0].IdempotencyKey || authorization != "Bearer send-secret" {
		t.Fatalf("received=%#v", received)
	}
	if result.ExternalBatchID != "cloud-batch-7" || len(result.Tasks) != 1 || result.Tasks[0].ExternalTaskID != "cloud-task-11" {
		t.Fatalf("result=%#v", result)
	}
}

func TestJavDownloadCallbackRequiresDedicatedBearerToken(t *testing.T) {
	t.Setenv("JAVBOSS_CLOUD_DOWNLOAD_CALLBACK_TOKEN", "callback-secret")
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerJavDownloadCallbackRoutes(router)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/jav/magnet-queue/attempts/1", bytes.NewBufferString(`{"status":"submitted"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer wrong-secret")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestConfiguredJavDownloadSubmitterRejectsInvalidURL(t *testing.T) {
	t.Setenv("JAVBOSS_CLOUD_DOWNLOAD_URL", "not-a-url")
	if _, configured, err := configuredJavDownloadSubmitter(); err == nil || configured {
		t.Fatalf("configured=%v err=%v", configured, err)
	}
}

func TestConfiguredJavDownloadControllerDerivesReviewEndpoints(t *testing.T) {
	t.Setenv("JAVBOSS_CLOUD_DOWNLOAD_URL", "http://127.0.0.1:18081/v1/javboss/download-batches")
	t.Setenv("JAVBOSS_CLOUD_DOWNLOAD_REVIEW_URL", "")
	t.Setenv("JAVBOSS_CLOUD_DOWNLOAD_REVIEW_BATCH_URL", "")

	controller, configured, err := configuredJavDownloadController()
	if err != nil || !configured {
		t.Fatalf("configured=%v err=%v", configured, err)
	}
	resolved, ok := controller.(*httpJavDownloadController)
	if !ok {
		t.Fatalf("controller type=%T, want *httpJavDownloadController", controller)
	}
	if resolved.endpointTemplate != "http://127.0.0.1:18081/v1/javboss/download-attempts/{attempt_id}/review" {
		t.Fatalf("review endpoint=%q", resolved.endpointTemplate)
	}
	if resolved.batchEndpoint != "http://127.0.0.1:18081/v1/javboss/download-attempts/review-batch" {
		t.Fatalf("batch endpoint=%q", resolved.batchEndpoint)
	}
}

func TestExternalRejectedStatusIsTransportFailureNotQualityVerdict(t *testing.T) {
	if status := normalizeExternalJavDownloadStatus("rejected"); status != models.JavDownloadAttemptFailed {
		t.Fatalf("normalized status=%q, want transport failure", status)
	}
}

func TestSubmitJavDownloadBatchWithoutAdapterKeepsSelectionQueued(t *testing.T) {
	t.Setenv("JAVBOSS_CLOUD_DOWNLOAD_URL", "")
	database, err := db.Open(t.TempDir() + "/submitter.db")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	previousDB := common.DB
	common.DB = database
	t.Cleanup(func() {
		common.DB = previousDB
		if sqlDB, dbErr := database.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	work := models.Jav{Code: "SEND-001", NormalizedCode: "SEND001"}
	if err := database.Create(&work).Error; err != nil {
		t.Fatalf("create JAV: %v", err)
	}
	candidates, err := db.UpsertJavMagnetCandidates(context.Background(), work.ID, []jav.JavDBAppMagnet{{Hash: "send-hash"}})
	if err != nil {
		t.Fatalf("create candidate: %v", err)
	}
	if _, err := db.SelectJavMagnet(context.Background(), work.ID, candidates[0].ID); err != nil {
		t.Fatalf("select candidate: %v", err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/jav/magnet-queue/submit", submitJavDownloadBatch)
	recorder := httptest.NewRecorder()
	body, err := json.Marshal(javDownloadBatchRequest{JavIDs: []int64{work.ID}})
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/jav/magnet-queue/submit", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var batchCount int64
	if err := database.Model(&models.JavDownloadBatch{}).Count(&batchCount).Error; err != nil || batchCount != 0 {
		t.Fatalf("batch count=%d err=%v", batchCount, err)
	}
	queue, total, err := db.ListJavDownloadQueue(context.Background(), 10, 0)
	if err != nil || total != 1 || len(queue) != 1 {
		t.Fatalf("queue total=%d items=%#v err=%v", total, queue, err)
	}
}
