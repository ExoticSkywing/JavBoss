package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"javboss/internal/common/logging"
	dbpkg "javboss/internal/db"
	"javboss/internal/jav"
)

func getJavTrailer(c *gin.Context) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id <= 0 {
		respondLocalizedError(c, http.StatusBadRequest, "JAV 作品 ID 无效", "Invalid JAV item ID")
		return
	}
	item, err := dbpkg.GetJav(c.Request.Context(), id, parseDirectoryIDs(c.Query("directory_ids")))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondLocalizedError(c, http.StatusNotFound, "JAV 作品不存在", "JAV item was not found")
			return
		}
		logging.Error("load JAV for trailer id=%d: %v", id, err)
		respondLocalizedError(c, http.StatusInternalServerError, "读取 JAV 作品失败", "Failed to load JAV item")
		return
	}
	trailer, err := jav.DefaultTrailerResolver().Resolve(
		c.Request.Context(),
		item.Code,
		queryBool(c, "refresh", false),
	)
	if err != nil {
		if errors.Is(err, jav.ErrTrailerNotFound) {
			respondLocalizedError(c, http.StatusNotFound, "未找到该作品的预告片", "No trailer was found for this JAV item")
			return
		}
		logging.Error("lookup JAV trailer code=%s: %v", item.Code, err)
		respondLocalizedError(c, http.StatusBadGateway, "预告片来源暂时不可用，请稍后重试", "Trailer providers are temporarily unavailable; try again later")
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, trailer)
}
