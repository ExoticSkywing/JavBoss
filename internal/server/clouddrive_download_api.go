package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"javboss/internal/clouddrive"
	"javboss/internal/db"
	"javboss/internal/jav"
	"javboss/internal/models"
	"javboss/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type cloudDriveSettingsResponse struct {
	Address         string `json:"address"`
	RemoteFolder    string `json:"remote_folder"`
	DirectoryID     *int64 `json:"directory_id"`
	Enabled         bool   `json:"enabled"`
	TokenConfigured bool   `json:"token_configured"`
}

func getCloudDriveSettings(c *gin.Context) {
	settings, err := db.GetCloudDriveSettings(c.Request.Context())
	if err != nil {
		respondLocalizedError(c, http.StatusInternalServerError, "读取 CloudDrive2 配置失败", "Failed to load CloudDrive2 settings")
		return
	}
	c.JSON(http.StatusOK, cloudDriveSettingsPayload(settings))
}

func updateCloudDriveSettings(c *gin.Context) {
	var request struct {
		Address       string  `json:"address"`
		APIToken      *string `json:"api_token"`
		ClearAPIToken bool    `json:"clear_api_token"`
		RemoteFolder  string  `json:"remote_folder"`
		DirectoryID   *int64  `json:"directory_id"`
		Enabled       bool    `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		respondLocalizedError(c, http.StatusBadRequest, "CloudDrive2 配置格式不正确", "Invalid CloudDrive2 settings")
		return
	}
	current, err := db.GetCloudDriveSettings(c.Request.Context())
	if err != nil {
		respondLocalizedError(c, http.StatusInternalServerError, "读取 CloudDrive2 配置失败", "Failed to load CloudDrive2 settings")
		return
	}
	address := strings.TrimSpace(request.Address)
	remoteFolder := strings.TrimSpace(request.RemoteFolder)
	if len(address) > 500 || len(remoteFolder) > 2000 {
		respondLocalizedError(c, http.StatusBadRequest, "CloudDrive2 地址或目录过长", "CloudDrive2 address or folder is too long")
		return
	}
	token := current.APIToken
	if request.ClearAPIToken {
		token = ""
	} else if request.APIToken != nil && strings.TrimSpace(*request.APIToken) != "" {
		token = strings.TrimSpace(*request.APIToken)
	}
	if len(token) > 16384 {
		respondLocalizedError(c, http.StatusBadRequest, "CloudDrive2 API Token 过长", "CloudDrive2 API token is too long")
		return
	}
	if request.DirectoryID != nil {
		directory, loadErr := db.GetDirectory(c.Request.Context(), *request.DirectoryID)
		if loadErr != nil || directory == nil || directory.IsDelete {
			respondLocalizedError(c, http.StatusBadRequest, "本地下载目录不存在", "The local download directory does not exist")
			return
		}
	}
	if request.Enabled && (address == "" || token == "" || remoteFolder == "" || request.DirectoryID == nil) {
		respondLocalizedError(c, http.StatusBadRequest, "请完整配置地址、Token、云端目录和本地目录", "Configure the address, token, remote folder, and local directory")
		return
	}
	settings := models.CloudDriveSettings{
		ID: 1, Address: address, APIToken: token, RemoteFolder: remoteFolder,
		DirectoryID: request.DirectoryID, Enabled: request.Enabled,
	}
	if err := db.SaveCloudDriveSettings(c.Request.Context(), &settings); err != nil {
		respondLocalizedError(c, http.StatusInternalServerError, "保存 CloudDrive2 配置失败", "Failed to save CloudDrive2 settings")
		return
	}
	service.WakeCloudDriveDownloadManager()
	c.JSON(http.StatusOK, cloudDriveSettingsPayload(&settings))
}

func testCloudDriveSettings(c *gin.Context) {
	settings, err := db.GetCloudDriveSettings(c.Request.Context())
	if err != nil || settings.Address == "" || settings.APIToken == "" || settings.RemoteFolder == "" {
		respondLocalizedError(c, http.StatusBadRequest, "请先保存完整的 CloudDrive2 配置", "Save complete CloudDrive2 settings first")
		return
	}
	client, err := clouddrive.NewClient(settings.Address, settings.APIToken)
	if err != nil {
		respondLocalizedError(c, http.StatusBadRequest, "CloudDrive2 地址无效", "The CloudDrive2 address is invalid")
		return
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	info, err := client.Test(ctx, settings.RemoteFolder)
	if err != nil {
		respondLocalizedError(c, http.StatusBadGateway, "无法连接 CloudDrive2 或验证目标目录", "Could not connect to CloudDrive2 or validate the target folder")
		return
	}
	missing := make([]string, 0, 5)
	if !info.CanList {
		missing = append(missing, "allow_list")
	}
	if !info.CanCreateFolder {
		missing = append(missing, "allow_create_folder")
	}
	if !info.CanRead {
		missing = append(missing, "allow_read")
	}
	if !info.CanAddOffline {
		missing = append(missing, "allow_add_offline_download")
	}
	if !info.CanListOffline {
		missing = append(missing, "allow_list_offline_downloads")
	}
	if len(missing) > 0 {
		respondLocalizedError(
			c,
			http.StatusUnprocessableEntity,
			"CloudDrive2 API Token 缺少权限: "+strings.Join(missing, ", "),
			"CloudDrive2 API token is missing permissions: "+strings.Join(missing, ", "),
		)
		return
	}
	if info.Folder == nil || !info.Folder.GetIsDirectory() || !info.Folder.GetCanOfflineDownload() {
		respondLocalizedError(c, http.StatusUnprocessableEntity, "CloudDrive2 目标目录不支持离线下载", "The CloudDrive2 target folder does not support offline downloads")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok": true, "user_name": info.UserName, "system_ready": info.SystemReady,
		"token_root": info.TokenRoot, "folder": info.Folder.GetFullPathName(),
	})
}

func cloudDriveSettingsPayload(settings *models.CloudDriveSettings) cloudDriveSettingsResponse {
	return cloudDriveSettingsResponse{
		Address: settings.Address, RemoteFolder: settings.RemoteFolder, DirectoryID: settings.DirectoryID,
		Enabled: settings.Enabled, TokenConfigured: strings.TrimSpace(settings.APIToken) != "",
	}
}

func listCloudDriveDownloads(c *gin.Context) {
	jobs, err := db.ListCloudDriveDownloads(c.Request.Context(), queryInt(c, "limit", 100))
	if err != nil {
		respondLocalizedError(c, http.StatusInternalServerError, "读取下载队列失败", "Failed to load the download queue")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": jobs})
}

func createCloudDriveDownload(c *gin.Context) {
	itemID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || itemID <= 0 {
		respondLocalizedError(c, http.StatusBadRequest, "作品 ID 不正确", "Invalid item ID")
		return
	}
	var request struct {
		MagnetURL   string `json:"magnet_url"`
		DirectoryID *int64 `json:"directory_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		respondLocalizedError(c, http.StatusBadRequest, "下载请求格式不正确", "Invalid download request")
		return
	}
	settings, err := db.GetCloudDriveSettings(c.Request.Context())
	if err != nil || !settings.Enabled {
		respondLocalizedError(c, http.StatusConflict, "CloudDrive2 下载尚未启用", "CloudDrive2 downloading is not enabled")
		return
	}
	directoryID := settings.DirectoryID
	if request.DirectoryID != nil {
		directoryID = request.DirectoryID
	}
	if directoryID == nil || *directoryID <= 0 {
		respondLocalizedError(c, http.StatusBadRequest, "请选择本地下载目录", "Select a local download directory")
		return
	}
	directory, err := db.GetDirectory(c.Request.Context(), *directoryID)
	if err != nil || directory == nil || directory.IsDelete {
		respondLocalizedError(c, http.StatusBadRequest, "本地下载目录不存在", "The local download directory does not exist")
		return
	}
	item, err := db.GetJavDiscoveryItem(c.Request.Context(), itemID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || strings.Contains(err.Error(), gorm.ErrRecordNotFound.Error()) {
			respondLocalizedError(c, http.StatusNotFound, "发现作品不存在", "Discovery item not found")
			return
		}
		respondLocalizedError(c, http.StatusInternalServerError, "读取发现作品失败", "Failed to read the discovery item")
		return
	}
	magnetURL := strings.TrimSpace(request.MagnetURL)
	var magnets []jav.JavBusMagnetLink
	if err := json.Unmarshal([]byte(item.MagnetLinksJSON), &magnets); err != nil {
		respondLocalizedError(c, http.StatusConflict, "请先打开作品详情加载磁力链接", "Open the item details to load magnet links first")
		return
	}
	magnetName := ""
	allowed := false
	for _, magnet := range magnets {
		if strings.TrimSpace(magnet.URL) == magnetURL {
			allowed = true
			magnetName = strings.TrimSpace(magnet.Name)
			break
		}
	}
	if !allowed {
		respondLocalizedError(c, http.StatusBadRequest, "磁力链接不属于该发现作品", "The magnet link does not belong to this discovery item")
		return
	}
	infoHash, err := service.ParseMagnetInfoHash(magnetURL)
	if err != nil {
		respondLocalizedError(c, http.StatusBadRequest, "磁力链接格式不正确", "Invalid magnet link")
		return
	}
	job := models.JavDiscoveryDownload{
		JavDiscoveryItemID: itemID, DirectoryID: *directoryID, InfoHash: infoHash,
		MagnetURL: magnetURL, MagnetName: magnetName,
	}
	if err := db.CreateCloudDriveDownload(c.Request.Context(), &job); err != nil {
		if errors.Is(err, db.ErrCloudDriveDownloadExists) {
			respondLocalizedError(c, http.StatusConflict, "该磁力已经在此目录的下载队列中", "This magnet is already queued for the selected directory")
			return
		}
		respondLocalizedError(c, http.StatusInternalServerError, "创建下载任务失败", "Failed to create the download job")
		return
	}
	service.WakeCloudDriveDownloadManager()
	c.JSON(http.StatusCreated, job)
}

func retryCloudDriveDownload(c *gin.Context) {
	id, ok := cloudDriveDownloadID(c)
	if !ok {
		return
	}
	if err := db.RetryCloudDriveDownload(c.Request.Context(), id); err != nil {
		respondLocalizedError(c, http.StatusConflict, "该任务当前不能重试", "The job cannot be retried in its current state")
		return
	}
	service.WakeCloudDriveDownloadManager()
	c.Status(http.StatusNoContent)
}

func cancelCloudDriveDownload(c *gin.Context) {
	id, ok := cloudDriveDownloadID(c)
	if !ok {
		return
	}
	if err := db.CancelCloudDriveDownload(c.Request.Context(), id); err != nil {
		respondLocalizedError(c, http.StatusConflict, "该任务当前不能取消", "The job cannot be canceled in its current state")
		return
	}
	service.CancelCloudDriveDownload(id)
	c.Status(http.StatusNoContent)
}

func deleteCloudDriveDownload(c *gin.Context) {
	id, ok := cloudDriveDownloadID(c)
	if !ok {
		return
	}
	if err := db.DeleteCloudDriveDownload(c.Request.Context(), id); err != nil {
		respondLocalizedError(c, http.StatusConflict, "只能删除已结束的下载任务", "Only finished download jobs can be deleted")
		return
	}
	c.Status(http.StatusNoContent)
}

func cloudDriveDownloadID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		respondLocalizedError(c, http.StatusBadRequest, "下载任务 ID 不正确", "Invalid download job ID")
		return 0, false
	}
	return id, true
}
