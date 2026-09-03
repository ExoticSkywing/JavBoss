package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"javboss/internal/common"
	dbpkg "javboss/internal/db"
	"javboss/internal/models"
)

func TestTelegramJavInputIsAuthenticatedAndIdempotent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("JAVBOSS_INPUT_TOKEN", "telegram-input-secret")
	database, err := dbpkg.Open(filepath.Join(t.TempDir(), "telegram-jav-input.db"))
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
	registerJavTelegramInputRoutes(router)
	request := func(token, key, raw string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/integrations/telegram/jav/input-batches", strings.NewReader(`{"raw_input":`+strconvQuote(raw)+`}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Idempotency-Key", key)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}

	unauthorized := request("wrong", "telegram:1:2", "TG-001")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	first := request("telegram-input-secret", "telegram:1:2", "TG-001\nTG-001")
	if first.Code != http.StatusCreated {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	var created models.JavInputBatch
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created batch: %v", err)
	}
	if created.Source != "telegram" || created.ExternalRequestID == nil || *created.ExternalRequestID != "telegram:1:2" {
		t.Fatalf("created source identity = %#v", created)
	}

	second := request("telegram-input-secret", "telegram:1:2", "DIFFERENT-999")
	if second.Code != http.StatusOK {
		t.Fatalf("second status=%d body=%s", second.Code, second.Body.String())
	}
	var replayed models.JavInputBatch
	if err := json.Unmarshal(second.Body.Bytes(), &replayed); err != nil {
		t.Fatalf("decode replayed batch: %v", err)
	}
	if replayed.ID != created.ID || replayed.RawInput != created.RawInput {
		t.Fatalf("replayed batch = %#v, want original %#v", replayed, created)
	}
	var count int64
	if err := database.Model(&models.JavInputBatch{}).Count(&count).Error; err != nil {
		t.Fatalf("count input batches: %v", err)
	}
	if count != 1 {
		t.Fatalf("input batch count=%d, want 1", count)
	}
}

func strconvQuote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
