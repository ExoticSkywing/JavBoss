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
	AttemptID      int64    `json:"attempt_id"`
	IdempotencyKey string   `json:"idempotency_key"`
	ExternalTaskID string   `json:"external_task_id"`
	Status         string   `json:"status"`
	Error          string   `json:"error"`
	ResultPaths    []string `json:"result_paths"`
}

type javDownloadSubmitter interface {
	Submit(context.Context, int64, []db.JavDownloadSubmissionItem) (javDownloadSubmissionResult, error)
}

type javDownloadController interface {
	Review(context.Context, int64, string) (javDownloadSubmissionTask, error)
	ReviewBatch(context.Context, []db.JavQualityReviewSubmission) (javDownloadReviewBatchResult, error)
}

type javDownloadReviewBatchResult struct {
	Tasks   []javDownloadSubmissionTask
	Cleanup map[string]int `json:"cleanup,omitempty"`
}

type httpJavDownloadSubmitter struct {
	endpoint string
	token    string
	client   *http.Client
}

type httpJavDownloadController struct {
	endpointTemplate string
	batchEndpoint    string
	token            string
	client           *http.Client
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

func configuredJavDownloadController() (javDownloadController, bool, error) {
	batchRaw := strings.TrimSpace(os.Getenv("JAVBOSS_CLOUD_DOWNLOAD_REVIEW_BATCH_URL"))
	raw := strings.TrimSpace(os.Getenv("JAVBOSS_CLOUD_DOWNLOAD_REVIEW_URL"))
	if raw == "" {
		submitURL := strings.TrimSpace(os.Getenv("JAVBOSS_CLOUD_DOWNLOAD_URL"))
		if submitURL == "" {
			return nil, false, nil
		}
		parsed, err := url.Parse(submitURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return nil, false, fmt.Errorf("JAVBOSS_CLOUD_DOWNLOAD_URL must be an absolute http(s) URL")
		}
		path := strings.TrimRight(parsed.Path, "/")
		if !strings.HasSuffix(path, "/download-batches") {
			return nil, false, errors.New("JAVBOSS_CLOUD_DOWNLOAD_REVIEW_URL is required for a non-standard submit URL")
		}
		parsed.Path = strings.TrimSuffix(path, "/download-batches") + "/download-attempts/{attempt_id}/review"
		raw = javReviewURLString(parsed)
		if batchRaw == "" {
			parsed.Path = strings.TrimSuffix(path, "/download-batches") + "/download-attempts/review-batch"
			batchRaw = parsed.String()
		}
	}
	// url.URL.String escapes braces in paths. Accept an explicitly encoded
	// placeholder as well, then normalize it so Review can substitute the
	// attempt ID safely.
	raw = strings.ReplaceAll(raw, "%7Battempt_id%7D", "{attempt_id}")
	if !strings.Contains(raw, "{attempt_id}") {
		return nil, false, errors.New("JAVBOSS_CLOUD_DOWNLOAD_REVIEW_URL must contain {attempt_id}")
	}
	probe := strings.Replace(raw, "{attempt_id}", "1", 1)
	parsed, err := url.Parse(probe)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, false, errors.New("JAVBOSS_CLOUD_DOWNLOAD_REVIEW_URL must be an absolute http(s) URL")
	}
	if batchRaw == "" {
		batchParsed := parsed
		batchParsed.Path = strings.Replace(batchParsed.Path, "/download-attempts/1/review", "/download-attempts/review-batch", 1)
		batchRaw = batchParsed.String()
	}
	batchParsed, err := url.Parse(batchRaw)
	if err != nil || batchParsed.Host == "" || (batchParsed.Scheme != "http" && batchParsed.Scheme != "https") {
		return nil, false, errors.New("JAVBOSS_CLOUD_DOWNLOAD_REVIEW_BATCH_URL must be an absolute http(s) URL")
	}
	return &httpJavDownloadController{
		endpointTemplate: raw,
		batchEndpoint:    strings.TrimRight(batchParsed.String(), "/"),
		token:            strings.TrimSpace(os.Getenv("JAVBOSS_CLOUD_DOWNLOAD_TOKEN")),
		client:           &http.Client{Timeout: 60 * time.Second},
	}, true, nil
}

func javReviewURLString(parsed *url.URL) string {
	if parsed == nil {
		return ""
	}
	parsed.RawPath = ""
	return strings.NewReplacer("%7B", "{", "%7D", "}").Replace(parsed.String())
}

func (s *httpJavDownloadController) Review(ctx context.Context, attemptID int64, decision string) (javDownloadSubmissionTask, error) {
	if s == nil || attemptID <= 0 {
		return javDownloadSubmissionTask{}, errors.New("download review attempt id is required")
	}
	body, err := json.Marshal(map[string]string{"decision": decision})
	if err != nil {
		return javDownloadSubmissionTask{}, fmt.Errorf("encode cloud download review: %w", err)
	}
	endpoint := strings.Replace(s.endpointTemplate, "{attempt_id}", fmt.Sprintf("%d", attemptID), 1)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return javDownloadSubmissionTask{}, fmt.Errorf("create cloud download review request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return javDownloadSubmissionTask{}, fmt.Errorf("send cloud download review: %w", err)
	}
	defer resp.Body.Close()
	var payload struct {
		Task  javDownloadSubmissionTask `json:"task"`
		Error string                    `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return javDownloadSubmissionTask{}, fmt.Errorf("decode cloud download review response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		if strings.TrimSpace(payload.Error) != "" {
			return javDownloadSubmissionTask{}, errors.New(payload.Error)
		}
		return javDownloadSubmissionTask{}, fmt.Errorf("cloud download review returned HTTP %d", resp.StatusCode)
	}
	return payload.Task, nil
}

func (s *httpJavDownloadController) ReviewBatch(ctx context.Context, submissions []db.JavQualityReviewSubmission) (javDownloadReviewBatchResult, error) {
	if s == nil || strings.TrimSpace(s.batchEndpoint) == "" {
		return javDownloadReviewBatchResult{}, errors.New("batch download review endpoint is not configured")
	}
	if len(submissions) == 0 {
		return javDownloadReviewBatchResult{}, errors.New("at least one quality review is required")
	}
	type reviewItem struct {
		AttemptID int64  `json:"attempt_id"`
		Decision  string `json:"decision"`
	}
	items := make([]reviewItem, 0, len(submissions))
	for _, submission := range submissions {
		items = append(items, reviewItem{AttemptID: submission.AttemptID, Decision: submission.Decision})
	}
	body, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		return javDownloadReviewBatchResult{}, fmt.Errorf("encode cloud download batch review: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.batchEndpoint, bytes.NewReader(body))
	if err != nil {
		return javDownloadReviewBatchResult{}, fmt.Errorf("create cloud download batch review request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return javDownloadReviewBatchResult{}, fmt.Errorf("send cloud download batch review: %w", err)
	}
	defer resp.Body.Close()
	var payload struct {
		Items   []javDownloadSubmissionTask `json:"items"`
		Cleanup map[string]int              `json:"cleanup"`
		Error   string                      `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return javDownloadReviewBatchResult{}, fmt.Errorf("decode cloud download batch review response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		if strings.TrimSpace(payload.Error) != "" {
			return javDownloadReviewBatchResult{}, errors.New(payload.Error)
		}
		return javDownloadReviewBatchResult{}, fmt.Errorf("cloud download batch review returned HTTP %d", resp.StatusCode)
	}
	return javDownloadReviewBatchResult{Tasks: payload.Items, Cleanup: payload.Cleanup}, nil
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
