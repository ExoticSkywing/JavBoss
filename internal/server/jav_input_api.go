package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"javboss/internal/jav"
)

const maxJavInputNumbers = 50

type javInputResolveRequest struct {
	Numbers []string `json:"numbers"`
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
