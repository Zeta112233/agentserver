package llmproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// handleGeminiProxy proxies Gemini API requests, recording token usage and trace data.
func (s *Server) handleGeminiProxy(w http.ResponseWriter, r *http.Request) {
	// 1. Validate proxy token.
	// The go-genai SDK sends credentials via x-goog-api-key, but clients
	// may also use x-api-key (consistent with the Anthropic proxy flow).
	proxyToken := r.Header.Get("x-goog-api-key")
	if proxyToken == "" {
		proxyToken = r.Header.Get("x-api-key")
	}
	if proxyToken == "" {
		http.Error(w, "missing api key", http.StatusUnauthorized)
		return
	}

	sbx, err := s.ValidateProxyToken(r.Context(), proxyToken)
	if err != nil {
		s.logger.Error("token validation failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if sbx == nil {
		http.Error(w, "invalid api key", http.StatusUnauthorized)
		return
	}
	if sbx.Status != "running" && sbx.Status != "creating" {
		http.Error(w, "sandbox not active", http.StatusForbidden)
		return
	}

	// 2. Determine upstream target.
	targetURL := s.config.GeminiBaseURL
	useModelserver := sbx.ModelserverUpstreamURL != ""
	if useModelserver {
		targetURL = sbx.ModelserverUpstreamURL
	} else if s.config.GeminiAPIKey == "" {
		http.Error(w, "gemini not configured", http.StatusServiceUnavailable)
		return
	}

	// 3. Check RPD quota (only for generate endpoints, skip for modelserver).
	isGenerateEndpoint := strings.Contains(r.URL.Path, ":generateContent") || strings.Contains(r.URL.Path, ":streamGenerateContent")
	if isGenerateEndpoint && !useModelserver {
		if exceeded, current, max := s.checkRPD(sbx.WorkspaceID); exceeded {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{
					"code":    429,
					"message": fmt.Sprintf("workspace requests per day quota exceeded (%d/%d)", current, max),
					"status":  "RESOURCE_EXHAUSTED",
				},
			})
			return
		}
	}

	// 4. Read body for trace extraction.
	bodyBytes, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 10<<20))
	if err != nil {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Detect streaming from URL path.
	isStreaming := strings.Contains(r.URL.Path, ":streamGenerateContent")

	// 5. Extract trace ID.
	traceID, source := s.ExtractGeminiTraceID(r, bodyBytes)
	requestID := GenerateGeminiRequestID()

	logger := s.logger.With(
		"trace_id", traceID,
		"request_id", requestID,
		"sandbox_id", sbx.ID,
		"workspace_id", sbx.WorkspaceID,
	)

	// 6. Persist trace (only for generate endpoints).
	if isGenerateEndpoint && s.store != nil {
		if _, err := s.store.GetOrCreateTrace(traceID, sbx.ID, sbx.WorkspaceID, source); err != nil {
			logger.Error("failed to create trace", "error", err)
		}
	}

	// 7. Set up reverse proxy.
	target, err := url.Parse(targetURL)
	if err != nil {
		logger.Error("invalid upstream URL", "error", err)
		http.Error(w, "invalid upstream URL", http.StatusInternalServerError)
		return
	}

	// 7a. Pre-fetch modelserver token if needed.
	var msToken string
	if useModelserver {
		var tokenErr error
		msToken, tokenErr = s.fetchModelserverToken(sbx.WorkspaceID)
		if tokenErr != nil {
			logger.Error("failed to get modelserver token", "error", tokenErr)
			http.Error(w, "modelserver token unavailable", http.StatusBadGateway)
			return
		}
	}

	startTime := time.Now()

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.URL.Path = r.URL.Path
			req.URL.RawQuery = r.URL.RawQuery
			req.Host = target.Host

			// Remove proxy token headers (client may have sent either).
			req.Header.Del("x-api-key")
			req.Header.Del("x-goog-api-key")

			if useModelserver {
				req.Header.Set("Authorization", "Bearer "+msToken)
			} else {
				req.Header.Set("x-goog-api-key", s.config.GeminiAPIKey)
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			if !isGenerateEndpoint {
				return nil
			}
			if isStreaming {
				return s.interceptGeminiStreaming(resp, sbx, traceID, requestID, logger, startTime)
			}
			return s.interceptGeminiNonStreaming(resp, sbx, traceID, requestID, logger, startTime)
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.Error("proxy error", "error", err)
			http.Error(w, "proxy error", http.StatusBadGateway)
		},
	}

	proxy.ServeHTTP(w, r)
}

// interceptGeminiNonStreaming reads the full response body, extracts usage, and records it.
func (s *Server) interceptGeminiNonStreaming(resp *http.Response, sbx *SandboxInfo, traceID, requestID string, logger *slog.Logger, startTime time.Time) error {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		logger.Error("failed to read response body", "error", err)
		resp.Body = io.NopCloser(bytes.NewReader(nil))
		return nil
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))

	model, usage, err := ParseGeminiNonStreamingResponse(body)
	if err != nil {
		logger.Warn("failed to parse gemini response", "error", err)
		return nil
	}

	duration := time.Since(startTime).Milliseconds()
	s.recordGeminiUsage(sbx, traceID, requestID, model, usage, false, duration, 0, logger)
	return nil
}

// interceptGeminiStreaming wraps the response body with a Gemini stream interceptor.
func (s *Server) interceptGeminiStreaming(resp *http.Response, sbx *SandboxInfo, traceID, requestID string, logger *slog.Logger, startTime time.Time) error {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}

	resp.Body = newGeminiStreamInterceptor(resp.Body, startTime, func(model string, usage GeminiUsageMetadata, ttft int64) {
		duration := time.Since(startTime).Milliseconds()
		s.recordGeminiUsage(sbx, traceID, requestID, model, usage, true, duration, ttft, logger)
	})
	return nil
}

// recordGeminiUsage persists a Gemini usage record and logs it.
func (s *Server) recordGeminiUsage(sbx *SandboxInfo, traceID, requestID, model string, usage GeminiUsageMetadata, streaming bool, duration, ttft int64, logger *slog.Logger) {
	logger.Info("gemini request completed",
		"model", model,
		"input_tokens", usage.PromptTokenCount,
		"output_tokens", usage.CandidatesTokenCount,
		"cache_read_input_tokens", usage.CachedContentTokenCount,
		"streaming", streaming,
		"duration", duration,
		"ttft", ttft,
	)

	if s.store == nil {
		return
	}

	u := TokenUsage{
		ID:                   requestID,
		TraceID:              traceID,
		SandboxID:            sbx.ID,
		WorkspaceID:          sbx.WorkspaceID,
		Provider:             "gemini",
		Model:                model,
		InputTokens:          usage.PromptTokenCount,
		OutputTokens:         usage.CandidatesTokenCount,
		CacheReadInputTokens: usage.CachedContentTokenCount,
		Streaming:            streaming,
		Duration:             duration,
		TTFT:                 ttft,
		CreatedAt:            time.Now(),
	}

	if err := s.store.RecordUsage(u); err != nil {
		logger.Error("failed to record usage", "error", err)
	}
	if err := s.store.UpdateTraceActivity(traceID); err != nil {
		logger.Error("failed to update trace activity", "error", err)
	}
}
