package media

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	mediaapp "github.com/chenyme/grok2api/backend/internal/application/media"
	"github.com/chenyme/grok2api/backend/internal/domain/admin"
	mediadomain "github.com/chenyme/grok2api/backend/internal/domain/media"
	"github.com/chenyme/grok2api/backend/internal/shared/response"
	"github.com/chenyme/grok2api/backend/internal/transport/http/middleware"
	"github.com/gin-gonic/gin"
)

func (h *Handler) inputUploads(c *gin.Context) (*mediaapp.InputUploadStore, uint64, bool) {
	value, ok := c.Get(middleware.AdminKey)
	identity, valid := value.(admin.Admin)
	if !ok || !valid || identity.ID == 0 {
		response.Error(c, 401, "adminUnauthorized", "管理员登录已失效")
		return nil, 0, false
	}
	h.uploadOnce.Do(func() {
		root := os.Getenv("GROK_INPUT_CHUNK_DIR")
		if root == "" {
			root = "./data/media/input-chunks"
		}
		h.uploadStore, h.uploadErr = mediaapp.NewInputUploadStore(root)
	})
	if h.uploadErr != nil {
		response.Error(c, 503, "uploadUnavailable", "上传存储暂不可用")
		return nil, 0, false
	}
	return h.uploadStore, identity.ID, true
}
func inputUploadError(c *gin.Context, err error) {
	status, code, message := 503, "uploadUnavailable", "上传存储暂不可用"
	switch {
	case errors.Is(err, mediaapp.ErrInputUpload):
		status, code, message = 404, "uploadNotFound", "上传会话不存在或已过期"
	case errors.Is(err, mediaapp.ErrInputUploadConflict):
		status, code, message = 409, "uploadConflict", "上传偏移或内容不一致"
	case errors.Is(err, mediaapp.ErrMediaCapacity):
		status, code, message = 507, "mediaCapacityExceeded", "媒体临时存储容量不足"
	case errors.Is(err, mediaapp.ErrInvalidImage), errors.Is(err, mediaapp.ErrInvalidVideoUpload):
		status, code, message = 400, "invalidMedia", "文件内容无效或格式不支持"
	}
	response.Error(c, status, code, message)
}
func writeInputUpload(c *gin.Context, status int, value mediaapp.InputUpload) {
	response.Success(c, status, gin.H{"uploadId": value.ID, "sizeBytes": value.Size, "offset": value.Offset, "chunkBytes": mediaapp.InputChunkBytes, "expiresAt": value.ExpiresAt})
}
func (h *Handler) createInputUpload(c *gin.Context) {
	store, owner, ok := h.inputUploads(c)
	if !ok {
		return
	}
	var input struct {
		Size int64  `json:"sizeBytes"`
		MIME string `json:"mimeType"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if c.ShouldBindJSON(&input) != nil || input.Size <= 0 || input.Size > mediadomain.MaxInputAssetBytes {
		response.Error(c, 400, "invalidRequest", "文件必须为 1 字节至 100 MiB")
		return
	}
	value, err := store.Create(owner, input.Size, input.MIME)
	if err != nil {
		inputUploadError(c, err)
		return
	}
	writeInputUpload(c, 201, value)
}
func (h *Handler) appendInputUpload(c *gin.Context) {
	store, owner, ok := h.inputUploads(c)
	if !ok {
		return
	}
	if !h.acquireIngest(c) {
		return
	}
	defer h.releaseIngest()
	offset, err := strconv.ParseInt(c.Query("offset"), 10, 64)
	if err != nil || offset < 0 {
		response.Error(c, 400, "invalidOffset", "需要有效上传偏移")
		return
	}
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, mediaapp.InputChunkBytes+1))
	if err != nil || len(data) == 0 || len(data) > mediaapp.InputChunkBytes {
		response.Error(c, 413, "chunkTooLarge", "每块必须为 1 字节至 8 MiB")
		return
	}
	value, err := store.Append(owner, c.Param("uploadId"), offset, data)
	if err != nil {
		inputUploadError(c, err)
		return
	}
	writeInputUpload(c, 200, value)
}
func (h *Handler) completeInputUpload(c *gin.Context) {
	store, owner, ok := h.inputUploads(c)
	if !ok {
		return
	}
	if !h.acquireIngest(c) {
		return
	}
	defer h.releaseIngest()
	value, err := store.Complete(owner, c.Param("uploadId"), func(mime string, reader io.Reader) (mediadomain.Asset, error) {
		if strings.HasPrefix(mime, "video/") {
			return h.service.SaveInputVideo(c.Request.Context(), mime, reader)
		}
		data, err := io.ReadAll(io.LimitReader(reader, mediadomain.MaxInputAssetBytes+1))
		if err != nil {
			return mediadomain.Asset{}, err
		}
		if int64(len(data)) > mediadomain.MaxInputAssetBytes || http.DetectContentType(data) != mime {
			return mediadomain.Asset{}, mediaapp.ErrInvalidImage
		}
		return h.service.SaveInputImage(c.Request.Context(), data)
	})
	if err != nil {
		inputUploadError(c, err)
		return
	}
	h.writeInputAsset(c, value)
}
func (h *Handler) deleteInputUpload(c *gin.Context) {
	store, owner, ok := h.inputUploads(c)
	if !ok {
		return
	}
	if err := store.Delete(owner, c.Param("uploadId")); err != nil {
		inputUploadError(c, err)
		return
	}
	response.Success(c, 200, gin.H{"deleted": true})
}
func (h *Handler) getInputContent(c *gin.Context) {
	value, body, err := h.service.OpenInputAsset(c.Request.Context(), c.Param("assetId"))
	if err != nil {
		response.Error(c, 404, "inputNotFound", "临时输入不存在或已过期")
		return
	}
	defer body.Close()
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.DataFromReader(200, value.SizeBytes, value.MIMEType, body, nil)
}
