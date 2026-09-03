package server

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"javboss/internal/db"
	"javboss/internal/service"
)

const maxJavExternalRequestIDBytes = 512

func registerJavTelegramInputRoutes(router gin.IRoutes) {
	router.POST(
		"/integrations/telegram/jav/input-batches",
		requireBearerEnv("JAVBOSS_INPUT_TOKEN", "番号输入集成密钥"),
		createTelegramJavInputBatch,
	)
}

func createTelegramJavInputBatch(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxJavInputRawBytes)
	var request javInputBatchRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondLocalizedError(c, http.StatusBadRequest, "番号输入格式无效或内容过大", "Invalid or oversized JAV input")
		return
	}
	if countNonEmptyJavInputLines(request.RawInput) > maxJavInputLines {
		respondLocalizedError(c, http.StatusBadRequest, "单个批次最多输入 5000 行", "A batch is limited to 5,000 lines")
		return
	}
	requestID := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if !strings.HasPrefix(requestID, "telegram:") || len(requestID) > maxJavExternalRequestIDBytes {
		respondLocalizedError(c, http.StatusBadRequest, "Telegram 幂等请求 ID 无效", "Invalid Telegram idempotency key")
		return
	}
	batch, created, err := db.CreateJavInputBatchFromSource(c.Request.Context(), request.RawInput, "telegram", requestID)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrJavInputEmpty):
			respondLocalizedError(c, http.StatusBadRequest, "至少输入一行番号", "Enter at least one line")
		case errors.Is(err, db.ErrJavInputNoCodes):
			respondLocalizedError(c, http.StatusBadRequest, "没有识别到番号，请检查输入", "No JAV code was recognized; check the input")
		default:
			respondLocalizedError(c, http.StatusInternalServerError, "保存 Telegram 番号输入失败", "Failed to save Telegram JAV input")
		}
		return
	}
	c.Header("Cache-Control", "no-store")
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, batch)
	if created && batch.AcceptedCount > 0 {
		service.RequestJavMetadataScan()
	}
}

func requireBearerEnv(name, label string) gin.HandlerFunc {
	return func(c *gin.Context) {
		expected := strings.TrimSpace(os.Getenv(name))
		if expected == "" {
			abortLocalizedError(c, http.StatusServiceUnavailable, "尚未配置"+label, "Integration token is not configured")
			return
		}
		provided := ""
		parts := strings.Fields(c.GetHeader("Authorization"))
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			provided = parts[1]
		}
		if len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			abortLocalizedError(c, http.StatusUnauthorized, label+"无效", "Invalid integration token")
			return
		}
		c.Next()
	}
}
