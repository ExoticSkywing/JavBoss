package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"javboss/internal/common"
	dbpkg "javboss/internal/db"
	"javboss/internal/models"
)

func TestJavInputBatchAPIStoresHistoryAndReturnsDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := dbpkg.Open(filepath.Join(t.TempDir(), "jav-input-api.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	previousDB := common.DB
	common.DB = database
	t.Cleanup(func() {
		common.DB = previousDB
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})

	router := gin.New()
	router.POST("/jav/input/batches", createJavInputBatch)
	router.GET("/jav/input/batches", listJavInputBatches)
	router.DELETE("/jav/input/batches", deleteAllJavInputBatches)
	router.GET("/jav/input/batches/:id", getJavInputBatch)
	router.DELETE("/jav/input/batches/:id", deleteJavInputBatch)
	router.GET("/jav/input/preprocessed", listJavInputPreprocessed)
	router.DELETE("/jav/input/preprocessed", clearJavInputPreprocessed)

	body := `{"raw_input":"  API-001 中文备注  \napi_001 第二次"}`
	request := httptest.NewRequest(http.MethodPost, "/jav/input/batches", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", response.Code, response.Body.String())
	}
	var created models.JavInputBatch
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created batch: %v", err)
	}
	if created.AcceptedCount != 1 || created.BatchDuplicateCount != 1 || len(created.Items) != 2 {
		t.Fatalf("created batch = %#v", created)
	}
	if created.Preview != "API-001 中文备注" {
		t.Fatalf("created preview = %q", created.Preview)
	}
	if created.Items[0].RawLine != "  API-001 中文备注  " {
		t.Fatalf("original line changed: %q", created.Items[0].RawLine)
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/jav/input/batches?page=1&page_size=20", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"total":1`) || !strings.Contains(response.Body.String(), `"preview":"API-001 中文备注"`) {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/jav/input/batches/"+strconv.FormatInt(created.ID, 10), nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "中文备注") {
		t.Fatalf("detail status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/jav/input/preprocessed?page=1&page_size=20", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"total":1`) || !strings.Contains(response.Body.String(), `"code":"API-001"`) {
		t.Fatalf("preprocessed status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/jav/input/preprocessed", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"cleared_count":0`) {
		t.Fatalf("clear preprocessed status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/jav/input/preprocessed?page=1&page_size=20", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"total":1`) {
		t.Fatalf("preprocessed after clear status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/jav/input/batches/"+strconv.FormatInt(created.ID, 10), nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/jav/input/preprocessed?page=1&page_size=20", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"total":1`) {
		t.Fatalf("deleting input receipt released the work: status=%d body=%s", response.Code, response.Body.String())
	}
	var javCount int64
	if err := database.Model(&models.Jav{}).Count(&javCount).Error; err != nil {
		t.Fatalf("count canonical works: %v", err)
	}
	if javCount != 1 {
		t.Fatalf("canonical work count = %d, want 1", javCount)
	}

}

func TestJavInputBatchAPIRejectsEmptyInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/jav/input/batches", createJavInputBatch)
	request := httptest.NewRequest(http.MethodPost, "/jav/input/batches", strings.NewReader(`{"raw_input":" \n "}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("empty input status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestJavInputBatchAPIRejectsInputWithoutRecognizableCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := dbpkg.Open(filepath.Join(t.TempDir(), "jav-input-no-code.db"))
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

	router := gin.New()
	router.POST("/jav/input/batches", createJavInputBatch)
	request := httptest.NewRequest(
		http.MethodPost,
		"/jav/input/batches",
		strings.NewReader(`{"raw_input":"这里只是说明文字\n2026-08-25"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "没有识别到番号") {
		t.Fatalf("no-code input status=%d body=%s", response.Code, response.Body.String())
	}
	var batchCount int64
	if err := database.Model(&models.JavInputBatch{}).Count(&batchCount).Error; err != nil {
		t.Fatalf("count batches: %v", err)
	}
	if batchCount != 0 {
		t.Fatalf("unrecognized input created %d batches", batchCount)
	}
}
