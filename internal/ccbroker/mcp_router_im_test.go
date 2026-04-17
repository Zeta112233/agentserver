package ccbroker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestRouter(imbridgeURL, executorURL string) *ToolRouter {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewToolRouter(ToolRouterConfig{
		ExecutorRegistryURL: executorURL,
		AgentserverURL:      "http://localhost:0",
		IMBridgeURL:         imbridgeURL,
		IMBridgeSecret:      "test-secret",
		WorkspaceDir:        "/tmp",
		SessionID:           "sess_test",
		WorkspaceID:         "ws_test",
		IMChannelID:         "ch_test",
		IMUserID:            "user_test",
	}, logger)
}

func TestRouteToIM_SendMessage_PostsToImbridge(t *testing.T) {
	var received struct {
		path   string
		body   map[string]string
		secret string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.path = r.URL.Path
		received.secret = r.Header.Get("X-Internal-Secret")
		_ = json.NewDecoder(r.Body).Decode(&received.body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"sent"}`))
	}))
	defer srv.Close()

	router := newTestRouter(srv.URL, "")
	res, err := router.routeToIM(context.Background(), "send_message",
		map[string]interface{}{"text": "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %+v", res)
	}
	if received.path != "/api/internal/imbridge/send" {
		t.Errorf("wrong path: %s", received.path)
	}
	if received.secret != "test-secret" {
		t.Errorf("wrong secret header: %q", received.secret)
	}
	if received.body["channel_id"] != "ch_test" || received.body["to_user_id"] != "user_test" || received.body["text"] != "hello" {
		t.Errorf("wrong body: %+v", received.body)
	}
}

func TestRouteToIM_SendMessage_EmptyText(t *testing.T) {
	router := newTestRouter("http://irrelevant", "")
	res, _ := router.routeToIM(context.Background(), "send_message", map[string]interface{}{})
	if !res.IsError {
		t.Fatalf("expected error for missing text")
	}
}

func TestRouteToIM_SendImage_Base64(t *testing.T) {
	var receivedBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"sent"}`))
	}))
	defer srv.Close()

	b64 := base64.StdEncoding.EncodeToString([]byte("fake-png-bytes"))
	router := newTestRouter(srv.URL, "")
	res, _ := router.routeToIM(context.Background(), "send_image", map[string]interface{}{
		"source":  b64,
		"format":  "png",
		"caption": "cat",
	})
	if res.IsError {
		t.Fatalf("expected success, got: %+v", res)
	}
	if receivedBody["image_base64"] != b64 {
		t.Errorf("image_base64 mismatch: %q vs %q", receivedBody["image_base64"], b64)
	}
	if receivedBody["format"] != "png" || receivedBody["caption"] != "cat" {
		t.Errorf("optional fields not propagated: %+v", receivedBody)
	}
}

func TestRouteToIM_SendImage_URL(t *testing.T) {
	imageBytes := []byte("\x89PNG\r\n\x1a\nfake")
	// URL server — returns image bytes.
	urlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(imageBytes)
	}))
	defer urlSrv.Close()

	var receivedBody map[string]string
	imbridgeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer imbridgeSrv.Close()

	router := newTestRouter(imbridgeSrv.URL, "")
	res, _ := router.routeToIM(context.Background(), "send_image", map[string]interface{}{
		"source": urlSrv.URL + "/cat.png",
	})
	if res.IsError {
		t.Fatalf("expected success, got: %+v", res)
	}
	decoded, err := base64.StdEncoding.DecodeString(receivedBody["image_base64"])
	if err != nil {
		t.Fatalf("image not base64-encoded: %v", err)
	}
	if string(decoded) != string(imageBytes) {
		t.Errorf("image bytes differ: got %q, want %q", decoded, imageBytes)
	}
}

func TestRouteToIM_SendImage_ExecutorPath(t *testing.T) {
	imageBytes := []byte("pixels")
	// Mock executor-registry — returns Read result base64-encoded.
	execSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ExecutorID string                 `json:"executor_id"`
			Tool       string                 `json:"tool"`
			Arguments  map[string]interface{} `json:"arguments"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.ExecutorID != "exe_dev" || req.Tool != "Read" {
			t.Errorf("wrong executor-registry call: %+v", req)
		}
		resp := map[string]interface{}{
			"output":    base64.StdEncoding.EncodeToString(imageBytes),
			"exit_code": 0,
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer execSrv.Close()

	var receivedBody map[string]string
	imbridgeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer imbridgeSrv.Close()

	router := newTestRouter(imbridgeSrv.URL, execSrv.URL)
	res, _ := router.routeToIM(context.Background(), "send_image", map[string]interface{}{
		"source": "exe_dev:/tmp/cat.png",
	})
	if res.IsError {
		t.Fatalf("expected success, got: %+v", res)
	}
	decoded, _ := base64.StdEncoding.DecodeString(receivedBody["image_base64"])
	if string(decoded) != string(imageBytes) {
		t.Errorf("bytes mismatch: %q vs %q", decoded, imageBytes)
	}
}

func TestRouteToIM_SendFile_NotSupported(t *testing.T) {
	router := newTestRouter("http://irrelevant", "")
	res, _ := router.routeToIM(context.Background(), "send_file", map[string]interface{}{
		"source":   "abc",
		"filename": "doc.pdf",
	})
	if !res.IsError {
		t.Fatalf("expected error for send_file, got: %+v", res)
	}
	if !strings.Contains(textOf(res), "not yet supported") {
		t.Errorf("expected 'not yet supported' error, got: %s", textOf(res))
	}
}

func TestRouteToIM_MissingIMContext(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := NewToolRouter(ToolRouterConfig{
		IMBridgeURL: "http://anything",
		// IMChannelID / IMUserID intentionally empty
	}, logger)
	res, _ := router.routeToIM(context.Background(), "send_message",
		map[string]interface{}{"text": "hi"})
	if !res.IsError {
		t.Fatalf("expected error when IM context missing")
	}
	if !strings.Contains(textOf(res), "IM-originated") {
		t.Errorf("expected IM-context error, got: %s", textOf(res))
	}
}

func TestRouteToIM_MissingImbridgeURL(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := NewToolRouter(ToolRouterConfig{
		IMChannelID: "ch",
		IMUserID:    "u",
	}, logger)
	res, _ := router.routeToIM(context.Background(), "send_message",
		map[string]interface{}{"text": "hi"})
	if !res.IsError {
		t.Fatalf("expected error when imbridgeURL unset")
	}
}

func TestRouteToIM_ImbridgeErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "provider failure", http.StatusBadGateway)
	}))
	defer srv.Close()

	router := newTestRouter(srv.URL, "")
	res, _ := router.routeToIM(context.Background(), "send_message",
		map[string]interface{}{"text": "hi"})
	if !res.IsError {
		t.Fatalf("expected error on imbridge 502")
	}
	if !strings.Contains(textOf(res), "502") {
		t.Errorf("expected status code in error, got: %s", textOf(res))
	}
}

func TestResolveMediaSource_Base64Fallback(t *testing.T) {
	router := newTestRouter("http://unused", "")
	bytes := []byte("abc123")
	data, err := router.resolveMediaSource(context.Background(), base64.StdEncoding.EncodeToString(bytes))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != string(bytes) {
		t.Errorf("got %q, want %q", data, bytes)
	}
}

func TestResolveMediaSource_RejectsInvalidBase64(t *testing.T) {
	router := newTestRouter("http://unused", "")
	_, err := router.resolveMediaSource(context.Background(), "!!!not-base64!!!")
	if err == nil {
		t.Fatalf("expected error for invalid base64")
	}
}

// textOf returns the first text content from an MCPToolResult for assertions.
func textOf(r *MCPToolResult) string {
	if r == nil || len(r.Content) == 0 {
		return ""
	}
	return r.Content[0].Text
}
