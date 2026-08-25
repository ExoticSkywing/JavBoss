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
	router.GET("/jav/input/batches/:id", getJavInputBatch)

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
	if created.Items[0].RawLine != "  API-001 中文备注  " {
		t.Fatalf("original line changed: %q", created.Items[0].RawLine)
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/jav/input/batches?page=1&page_size=20", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"total":1`) {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/jav/input/batches/"+strconv.FormatInt(created.ID, 10), nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "中文备注") {
		t.Fatalf("detail status=%d body=%s", response.Code, response.Body.String())
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
