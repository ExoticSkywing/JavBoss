package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"javboss/internal/db"
	"javboss/internal/jav"
)

const maxJavInputNumbers = 50

const (
	maxJavInputRawBytes = 2 << 20
	maxJavInputLines    = 5000
)

type javInputResolveRequest struct {
	Numbers []string `json:"numbers"`
}

type javInputBatchRequest struct {
	RawInput string `json:"raw_input"`
}

func createJavInputBatch(c *gin.Context) {
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
	batch, err := db.CreateJavInputBatch(c.Request.Context(), request.RawInput)
	if err != nil {
		if errors.Is(err, db.ErrJavInputEmpty) {
			respondLocalizedError(c, http.StatusBadRequest, "至少输入一行番号", "Enter at least one line")
			return
		}
		respondLocalizedError(c, http.StatusInternalServerError, "保存番号输入批次失败", "Failed to save JAV input batch")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, batch)
}

func listJavInputBatches(c *gin.Context) {
	page := positiveIntQuery(c.Query("page"), 1)
	pageSize := positiveIntQuery(c.Query("page_size"), 20)
	if pageSize > 100 {
		pageSize = 100
	}
	batches, total, err := db.ListJavInputBatches(c.Request.Context(), page, pageSize)
	if err != nil {
		respondLocalizedError(c, http.StatusInternalServerError, "读取番号输入历史失败", "Failed to load JAV input history")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"items":     batches,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

func getJavInputBatch(c *gin.Context) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id <= 0 {
		respondLocalizedError(c, http.StatusBadRequest, "番号输入批次 ID 无效", "Invalid JAV input batch ID")
		return
	}
	batch, err := db.GetJavInputBatch(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondLocalizedError(c, http.StatusNotFound, "番号输入批次不存在", "JAV input batch was not found")
			return
		}
		respondLocalizedError(c, http.StatusInternalServerError, "读取番号输入批次失败", "Failed to load JAV input batch")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, batch)
}

func positiveIntQuery(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func countNonEmptyJavInputLines(raw string) int {
	count := 0
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(strings.TrimSuffix(line, "\r")) != "" {
			count++
		}
	}
	return count
}

// resolveJavInput is deliberately read-only. It returns JavDB candidates for
// the user to inspect and manually confirm. Download submission is a later,
// separate stage.
func resolveJavInput(c *gin.Context) {
	var request javInputResolveRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondLocalizedError(c, http.StatusBadRequest, "番号请求格式无效", "Invalid JAV input request")
		return
	}
	numbers := normalizeJavInputNumbers(request.Numbers)
	if len(numbers) == 0 {
		respondLocalizedError(c, http.StatusBadRequest, "至少输入一个番号", "Enter at least one JAV code")
		return
	}
	if len(numbers) > maxJavInputNumbers {
		respondLocalizedError(c, http.StatusBadRequest, "单次最多查询 50 个番号", "A single lookup is limited to 50 JAV codes")
		return
	}
	response := jav.DefaultJavDBAppClient().ResolveBatch(c.Request.Context(), numbers)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, response)
}

func normalizeJavInputNumbers(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToUpper(strings.NewReplacer(" ", "", "_", "", "-", "").Replace(value))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}
