package openai

import (
	"context"
	"mime/multipart"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/logging"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

func imagesLogEntry(ctx context.Context, c *gin.Context) *log.Entry {
	requestID := logging.GetRequestID(ctx)
	if requestID == "" {
		requestID = logging.GetGinRequestID(c)
	}
	if requestID == "" {
		return log.NewEntry(log.StandardLogger())
	}
	return log.WithField("request_id", requestID)
}

func logImagesGenerationParsed(c *gin.Context, imageModel string, responseFormat string, stream bool, toolJSON []byte) {
	entry := imagesLogEntry(c.Request.Context(), c)
	entry.Infof(
		"openai images generation request parsed model=%s method=%s output_format=%s path=%s remote_ip=%s response_format=%s size=%s stream=%t",
		strings.TrimSpace(imageModel),
		strings.TrimSpace(c.Request.Method),
		toolString(toolJSON, "output_format"),
		imagesRequestPath(c),
		strings.TrimSpace(c.ClientIP()),
		strings.TrimSpace(responseFormat),
		toolString(toolJSON, "size"),
		stream,
	)
}

func logImagesEditMultipartParsed(c *gin.Context, imageModel string, responseFormat string, stream bool, toolJSON []byte, imageFiles []*multipart.FileHeader, maskFiles []*multipart.FileHeader) {
	filenames, contentTypes, totalBytes := summarizeMultipartFiles(imageFiles)
	entry := imagesLogEntry(c.Request.Context(), c)
	entry.Infof(
		"openai images edit multipart parsed model=%s image_content_types=%v image_count=%d image_filenames=%v image_total_bytes=%d mask_count=%d method=%s output_format=%s path=%s remote_ip=%s response_format=%s size=%s stream=%t",
		strings.TrimSpace(imageModel),
		contentTypes,
		len(imageFiles),
		filenames,
		totalBytes,
		len(maskFiles),
		strings.TrimSpace(c.Request.Method),
		toolString(toolJSON, "output_format"),
		imagesRequestPath(c),
		strings.TrimSpace(c.ClientIP()),
		strings.TrimSpace(responseFormat),
		toolString(toolJSON, "size"),
		stream,
	)
}

func logImagesEditJSONParsed(c *gin.Context, imageModel string, responseFormat string, stream bool, toolJSON []byte, images []string, maskCount int) {
	entry := imagesLogEntry(c.Request.Context(), c)
	entry.Infof(
		"openai images edit json parsed model=%s image_count=%d image_source_types=%v mask_count=%d method=%s output_format=%s path=%s remote_ip=%s response_format=%s size=%s stream=%t",
		strings.TrimSpace(imageModel),
		len(images),
		summarizeImageSources(images),
		maskCount,
		strings.TrimSpace(c.Request.Method),
		toolString(toolJSON, "output_format"),
		imagesRequestPath(c),
		strings.TrimSpace(c.ClientIP()),
		strings.TrimSpace(responseFormat),
		toolString(toolJSON, "size"),
		stream,
	)
}

func summarizeMultipartFiles(files []*multipart.FileHeader) ([]string, []string, int64) {
	contentTypes := make([]string, 0, len(files))
	filenames := make([]string, 0, len(files))
	seenContentTypes := make(map[string]struct{}, len(files))
	var totalBytes int64
	for _, file := range files {
		if file == nil {
			continue
		}
		if name := strings.TrimSpace(file.Filename); name != "" {
			filenames = append(filenames, name)
		}
		if contentType := strings.TrimSpace(file.Header.Get("Content-Type")); contentType != "" {
			if _, exists := seenContentTypes[contentType]; !exists {
				seenContentTypes[contentType] = struct{}{}
				contentTypes = append(contentTypes, contentType)
			}
		}
		if file.Size > 0 {
			totalBytes += file.Size
		}
	}
	return filenames, contentTypes, totalBytes
}

func summarizeImageSources(images []string) []string {
	sources := make([]string, 0, len(images))
	seen := make(map[string]struct{}, len(images))
	for _, image := range images {
		sourceType := imageSourceType(image)
		if _, exists := seen[sourceType]; exists {
			continue
		}
		seen[sourceType] = struct{}{}
		sources = append(sources, sourceType)
	}
	return sources
}

func imageSourceType(image string) string {
	image = strings.TrimSpace(image)
	if strings.HasPrefix(image, "data:") {
		mediaType := image[len("data:"):]
		if idx := strings.Index(mediaType, ";"); idx >= 0 {
			mediaType = mediaType[:idx]
		}
		if strings.TrimSpace(mediaType) != "" {
			return strings.TrimSpace(mediaType)
		}
		return "data_url"
	}
	if strings.HasPrefix(image, "http://") || strings.HasPrefix(image, "https://") {
		return "remote_url"
	}
	return "unknown"
}

func toolString(toolJSON []byte, path string) string {
	return strings.TrimSpace(gjson.GetBytes(toolJSON, path).String())
}

func imagesRequestPath(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if path := strings.TrimSpace(c.FullPath()); path != "" {
		return path
	}
	if c.Request == nil || c.Request.URL == nil {
		return ""
	}
	return strings.TrimSpace(c.Request.URL.Path)
}
