package llmproxy

import (
	"testing"
)

func TestParseGeminiNonStreamingResponse(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		wantModel     string
		wantInput     int64
		wantOutput    int64
		wantCacheRead int64
		wantErr       bool
	}{
		{
			name: "basic response",
			body: `{
				"candidates": [{"content": {"parts": [{"text": "hello"}]}}],
				"usageMetadata": {
					"promptTokenCount": 100,
					"candidatesTokenCount": 50,
					"cachedContentTokenCount": 10,
					"totalTokenCount": 160
				},
				"modelVersion": "gemini-2.5-flash"
			}`,
			wantModel:     "gemini-2.5-flash",
			wantInput:     100,
			wantOutput:    50,
			wantCacheRead: 10,
		},
		{
			name: "no usage metadata",
			body: `{
				"candidates": [{"content": {"parts": [{"text": "hello"}]}}],
				"modelVersion": "gemini-2.5-flash"
			}`,
			wantModel: "gemini-2.5-flash",
		},
		{
			name:    "invalid json",
			body:    `not json`,
			wantErr: true,
		},
		{
			name: "empty model version",
			body: `{
				"candidates": [],
				"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 3}
			}`,
			wantModel:  "",
			wantInput:  5,
			wantOutput: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, usage, err := ParseGeminiNonStreamingResponse([]byte(tt.body))
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if model != tt.wantModel {
				t.Errorf("model = %q, want %q", model, tt.wantModel)
			}
			if usage.PromptTokenCount != tt.wantInput {
				t.Errorf("input = %d, want %d", usage.PromptTokenCount, tt.wantInput)
			}
			if usage.CandidatesTokenCount != tt.wantOutput {
				t.Errorf("output = %d, want %d", usage.CandidatesTokenCount, tt.wantOutput)
			}
			if usage.CachedContentTokenCount != tt.wantCacheRead {
				t.Errorf("cacheRead = %d, want %d", usage.CachedContentTokenCount, tt.wantCacheRead)
			}
		})
	}
}

func TestParseGeminiStreamChunk(t *testing.T) {
	tests := []struct {
		name      string
		data      string
		wantModel string
		wantUsage bool
		wantParts bool
	}{
		{
			name: "chunk with usage and content",
			data: `{
				"candidates": [{"content": {"parts": [{"text": "hi"}], "role": "model"}}],
				"usageMetadata": {"promptTokenCount": 10, "candidatesTokenCount": 5},
				"modelVersion": "gemini-2.5-flash"
			}`,
			wantModel: "gemini-2.5-flash",
			wantUsage: true,
			wantParts: true,
		},
		{
			name: "chunk with usage only, no content parts",
			data: `{
				"usageMetadata": {"promptTokenCount": 10, "candidatesTokenCount": 50},
				"modelVersion": "gemini-2.5-flash"
			}`,
			wantModel: "gemini-2.5-flash",
			wantUsage: true,
			wantParts: false,
		},
		{
			name:      "empty chunk",
			data:      `{}`,
			wantModel: "",
			wantUsage: false,
			wantParts: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, usage, hasUsage, hasParts := ParseGeminiStreamChunk([]byte(tt.data))
			if model != tt.wantModel {
				t.Errorf("model = %q, want %q", model, tt.wantModel)
			}
			if hasUsage != tt.wantUsage {
				t.Errorf("hasUsage = %v, want %v", hasUsage, tt.wantUsage)
			}
			if hasParts != tt.wantParts {
				t.Errorf("hasParts = %v, want %v", hasParts, tt.wantParts)
			}
			if hasUsage && usage.PromptTokenCount == 0 {
				t.Error("expected non-zero promptTokenCount when hasUsage is true")
			}
		})
	}
}
