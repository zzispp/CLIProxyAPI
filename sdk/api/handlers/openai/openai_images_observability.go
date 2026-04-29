package openai

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/interfaces"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

const imagesFailurePreviewLimit = 256

type imagesResponseFailure struct {
	Code    string
	Event   string
	Message string
}

type imagesResponsesObserver struct {
	entry            *log.Entry
	startedAt        time.Time
	firstChunkAt     time.Time
	lastChunkAt      time.Time
	method           string
	path             string
	remoteIP         string
	mainModel        string
	responseFormat   string
	stream           bool
	byteCount        int
	chunkCount       int
	eventCount       int
	frameCount       int
	completed        bool
	failureLogged    bool
	lastErrorCode    string
	lastErrorMessage string
	lastEvent        string
}

func newImagesResponsesObserver(ctx context.Context, c *gin.Context, mainModel string, responseFormat string, stream bool) *imagesResponsesObserver {
	return &imagesResponsesObserver{
		entry:          imagesLogEntry(ctx, c),
		startedAt:      time.Now(),
		method:         strings.TrimSpace(c.Request.Method),
		path:           imagesRequestPath(c),
		remoteIP:       strings.TrimSpace(c.ClientIP()),
		mainModel:      strings.TrimSpace(mainModel),
		responseFormat: strings.TrimSpace(responseFormat),
		stream:         stream,
	}
}

func (o *imagesResponsesObserver) logUpstreamStarted(requestBytes int) {
	if o == nil {
		return
	}
	o.entry.Infof(
		"openai images responses upstream request started model=%s method=%s path=%s remote_ip=%s request_bytes=%d response_format=%s stream=%t",
		o.mainModel,
		o.method,
		o.path,
		o.remoteIP,
		requestBytes,
		o.responseFormat,
		o.stream,
	)
}

func (o *imagesResponsesObserver) observeChunk(chunk []byte) {
	if o == nil || len(chunk) == 0 {
		return
	}
	now := time.Now()
	if o.firstChunkAt.IsZero() {
		o.firstChunkAt = now
	}
	o.lastChunkAt = now
	o.byteCount += len(chunk)
	o.chunkCount++
}

func (o *imagesResponsesObserver) observeFrame() {
	if o == nil {
		return
	}
	o.frameCount++
}

func (o *imagesResponsesObserver) observeEvent(payload []byte) {
	if o == nil || len(payload) == 0 {
		return
	}
	o.eventCount++
	eventType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
	if eventType != "" {
		o.lastEvent = eventType
	}
	if eventType == "response.completed" {
		o.completed = true
		return
	}
	failure, ok := extractImagesResponseFailure(payload)
	if !ok {
		return
	}
	o.lastErrorCode = failure.Code
	o.lastErrorMessage = failure.Message
}

func (o *imagesResponsesObserver) logFailure(errMsg *interfaces.ErrorMessage) {
	if o == nil || o.failureLogged {
		return
	}
	o.failureLogged = true

	status := http.StatusInternalServerError
	if errMsg != nil && errMsg.StatusCode > 0 {
		status = errMsg.StatusCode
	}
	errorText := imagesErrorText(errMsg, status)
	if o.lastErrorMessage == "" {
		o.lastErrorMessage = errorText
	}

	mode := "non-streaming"
	if o.stream {
		mode = "streaming"
	}
	o.entry.Errorf(
		"openai images %s request failed model=%s error=%s duration_ms=%d method=%s path=%s remote_ip=%s response_format=%s status_code=%d upstream_bytes=%d upstream_chunks=%d upstream_completed=%t upstream_events=%d upstream_first_chunk_at=%s upstream_frames=%d upstream_last_chunk_at=%s upstream_last_error_code=%s upstream_last_error_message=%s upstream_last_event=%s",
		mode,
		o.mainModel,
		errorText,
		time.Since(o.startedAt).Milliseconds(),
		o.method,
		o.path,
		o.remoteIP,
		o.responseFormat,
		status,
		o.byteCount,
		o.chunkCount,
		o.completed,
		o.eventCount,
		imagesTimeText(o.firstChunkAt),
		o.frameCount,
		imagesTimeText(o.lastChunkAt),
		o.lastErrorCode,
		o.lastErrorMessage,
		o.lastEvent,
	)
}

func imagesTimeText(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.Format(time.RFC3339Nano)
}

func imagesErrorText(errMsg *interfaces.ErrorMessage, status int) string {
	if errMsg != nil && errMsg.Error != nil {
		if text := strings.TrimSpace(errMsg.Error.Error()); text != "" {
			return text
		}
	}
	return http.StatusText(status)
}

func buildImagesResponseFailedError(payload []byte) *interfaces.ErrorMessage {
	failure, ok := extractImagesResponseFailure(payload)
	if !ok {
		return nil
	}
	errorText := failure.Message
	if failure.Code != "" {
		errorText = failure.Code + ": " + failure.Message
	}
	return &interfaces.ErrorMessage{
		StatusCode: http.StatusBadGateway,
		Error:      fmt.Errorf("%s", errorText),
	}
}

func extractImagesResponseFailure(payload []byte) (imagesResponseFailure, bool) {
	eventType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
	if eventType != "response.failed" {
		return imagesResponseFailure{}, false
	}

	message := firstNonEmptyJSONValue(payload,
		"response.error.message",
		"error.message",
		"response.last_error.message",
		"last_error.message",
	)
	code := firstNonEmptyJSONValue(payload,
		"response.error.code",
		"error.code",
		"response.last_error.code",
		"last_error.code",
	)
	safetyViolations := firstStringArray(payload,
		"response.error.metadata.safety_violations",
		"error.metadata.safety_violations",
		"response.incomplete_details.safety_violations",
		"response.safety_violations",
		"safety_violations",
	)
	message = appendSafetyViolations(message, safetyViolations)
	if message == "" {
		message = "upstream response.failed payload=" + compactJSONPreview(payload, imagesFailurePreviewLimit)
	}

	return imagesResponseFailure{
		Code:    code,
		Event:   eventType,
		Message: message,
	}, true
}

func firstNonEmptyJSONValue(payload []byte, paths ...string) string {
	for _, path := range paths {
		if value := strings.TrimSpace(gjson.GetBytes(payload, path).String()); value != "" {
			return value
		}
	}
	return ""
}

func firstStringArray(payload []byte, paths ...string) []string {
	for _, path := range paths {
		node := gjson.GetBytes(payload, path)
		if !node.Exists() || !node.IsArray() {
			continue
		}
		values := make([]string, 0, len(node.Array()))
		for _, item := range node.Array() {
			if value := strings.TrimSpace(item.String()); value != "" {
				values = append(values, value)
			}
		}
		if len(values) > 0 {
			return values
		}
	}
	return nil
}

func appendSafetyViolations(message string, safetyViolations []string) string {
	if len(safetyViolations) == 0 {
		return strings.TrimSpace(message)
	}
	suffix := fmt.Sprintf("safety_violations=%v.", safetyViolations)
	message = strings.TrimSpace(message)
	if message == "" {
		return suffix
	}
	if strings.Contains(message, "safety_violations=") {
		return message
	}
	if strings.HasSuffix(message, ".") {
		return message + " " + suffix
	}
	return message + " " + suffix
}

func compactJSONPreview(payload []byte, limit int) string {
	preview := strings.Join(strings.Fields(string(payload)), " ")
	if limit <= 0 || len(preview) <= limit {
		return preview
	}
	return preview[:limit]
}
