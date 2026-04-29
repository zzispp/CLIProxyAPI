package openai

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/interfaces"
)

func TestBuildImagesResponseFailedErrorIncludesSafetyViolations(t *testing.T) {
	payload := []byte(`{"type":"response.failed","response":{"error":{"code":"moderation_blocked","message":"blocked by safety","metadata":{"safety_violations":["sexual"]}}}}`)

	errMsg := buildImagesResponseFailedError(payload)
	if errMsg == nil {
		t.Fatal("expected error message")
	}
	if errMsg.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", errMsg.StatusCode, http.StatusBadGateway)
	}

	got := errMsg.Error.Error()
	if !strings.Contains(got, "moderation_blocked: blocked by safety") {
		t.Fatalf("error = %q, want moderation details", got)
	}
	if !strings.Contains(got, "safety_violations=[sexual].") {
		t.Fatalf("error = %q, want safety violations", got)
	}
}

func TestCollectImagesFromResponsesStreamStopsOnResponseFailed(t *testing.T) {
	data := make(chan []byte, 1)
	errs := make(chan *interfaces.ErrorMessage)
	close(errs)

	data <- []byte("data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"moderation_blocked\",\"message\":\"blocked by safety\",\"metadata\":{\"safety_violations\":[\"sexual\"]}}}}\n\n")
	close(data)

	observer := &imagesResponsesObserver{}
	out, errMsg := collectImagesFromResponsesStream(context.Background(), data, errs, "b64_json", observer)
	if errMsg == nil {
		t.Fatal("expected error message")
	}
	if out != nil {
		t.Fatalf("output = %q, want nil", string(out))
	}

	got := errMsg.Error.Error()
	if !strings.Contains(got, "moderation_blocked: blocked by safety") {
		t.Fatalf("error = %q, want moderation details", got)
	}
	if observer.chunkCount != 1 {
		t.Fatalf("chunkCount = %d, want 1", observer.chunkCount)
	}
	if observer.frameCount != 1 {
		t.Fatalf("frameCount = %d, want 1", observer.frameCount)
	}
	if observer.eventCount != 1 {
		t.Fatalf("eventCount = %d, want 1", observer.eventCount)
	}
	if observer.lastEvent != "response.failed" {
		t.Fatalf("lastEvent = %q, want response.failed", observer.lastEvent)
	}
	if observer.lastErrorCode != "moderation_blocked" {
		t.Fatalf("lastErrorCode = %q, want moderation_blocked", observer.lastErrorCode)
	}
	if !strings.Contains(observer.lastErrorMessage, "safety_violations=[sexual].") {
		t.Fatalf("lastErrorMessage = %q, want safety violations", observer.lastErrorMessage)
	}
	if observer.completed {
		t.Fatal("expected completed to remain false")
	}
}
