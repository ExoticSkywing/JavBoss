package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"javboss/internal/db"
)

var errJavDownloadSubmitterNotConfigured = errors.New("cloud download submitter is not configured")

// javDownloadSubmissionResult is deliberately small. The external service may
// return one task id per item, or only a batch id; missing task ids are valid
// and can be filled by a later callback.
type javDownloadSubmissionResult struct {
	ExternalBatchID string
	Tasks           []javDownloadSubmissionTask
}

type javDownloadSubmissionTask struct {
	AttemptID      int64  `json:"attempt_id"`
	IdempotencyKey string `json:"idempotency_key"`
	ExternalTaskID string `json:"external_task_id"`
	Status         string `json:"status"`
	Error          string
}

type javDownloadSubmitter interface {
	Submit(context.Context, int64, []db.JavDownloadSubmissionItem) (javDownloadSubmissionResult, error)
}

type httpJavDownloadSubmitter struct {
	endpoint string
	token    string
	client   *http.Client
}

type javDownloadSubmitRequest struct {
	BatchID      int64                          `json:"batch_id"`
	CallbackPath string                         `json:"callback_path"`
	Items        []db.JavDownloadSubmissionItem `json:"items"`
}

type javDownloadSubmitResponse struct {
	BatchID         string                      `json:"batch_id"`
	ExternalBatchID string                      `json:"external_batch_id"`
	Tasks           []javDownloadSubmissionTask `json:"tasks"`
	Items           []javDownloadSubmissionTask `json:"items"`
}

func configuredJavDownloadSubmitter() (javDownloadSubmitter, bool, error) {
	raw := strings.TrimSpace(os.Getenv("JAVBOSS_CLOUD_DOWNLOAD_URL"))
	if raw == "" {
		return nil, false, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, false, fmt.Errorf("JAVBOSS_CLOUD_DOWNLOAD_URL must be an absolute http(s) URL")
	}
	return &httpJavDownloadSubmitter{
		endpoint: strings.TrimRight(parsed.String(), "/"),
		token:    strings.TrimSpace(os.Getenv("JAVBOSS_CLOUD_DOWNLOAD_TOKEN")),
		client:   &http.Client{Timeout: 20 * time.Second},
	}, true, nil
}

func (s *httpJavDownloadSubmitter) Submit(ctx context.Context, batchID int64, items []db.JavDownloadSubmissionItem) (javDownloadSubmissionResult, error) {
	if s == nil || strings.TrimSpace(s.endpoint) == "" {
		return javDownloadSubmissionResult{}, errJavDownloadSubmitterNotConfigured
	}
	body, err := json.Marshal(javDownloadSubmitRequest{BatchID: batchID, CallbackPath: "/jav/magnet-queue/attempts/{attempt_id}", Items: items})
	if err != nil {
		return javDownloadSubmissionResult{}, fmt.Errorf("encode cloud download request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return javDownloadSubmissionResult{}, fmt.Errorf("create cloud download request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return javDownloadSubmissionResult{}, fmt.Errorf("send cloud download request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return javDownloadSubmissionResult{}, fmt.Errorf("cloud download service returned HTTP %d", resp.StatusCode)
	}
	var payload javDownloadSubmitResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return javDownloadSubmissionResult{}, fmt.Errorf("decode cloud download response: %w", err)
	}
	tasks := payload.Tasks
	if len(tasks) == 0 {
		tasks = payload.Items
	}
	externalBatchID := strings.TrimSpace(payload.ExternalBatchID)
	if externalBatchID == "" {
		externalBatchID = strings.TrimSpace(payload.BatchID)
	}
	return javDownloadSubmissionResult{ExternalBatchID: externalBatchID, Tasks: tasks}, nil
}
