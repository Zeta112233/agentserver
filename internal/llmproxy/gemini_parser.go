package llmproxy

import "encoding/json"

// GeminiUsageMetadata holds token counts from a Gemini API response.
type GeminiUsageMetadata struct {
	PromptTokenCount        int64 `json:"promptTokenCount,omitempty"`
	CandidatesTokenCount    int64 `json:"candidatesTokenCount,omitempty"`
	CachedContentTokenCount int64 `json:"cachedContentTokenCount,omitempty"`
	TotalTokenCount         int64 `json:"totalTokenCount,omitempty"`
	ThoughtsTokenCount      int64 `json:"thoughtsTokenCount,omitempty"`
}

// geminiResponse is a minimal structure for Gemini generateContent responses.
type geminiResponse struct {
	Candidates []struct {
		Content *struct {
			Parts []json.RawMessage `json:"parts,omitempty"`
		} `json:"content,omitempty"`
	} `json:"candidates,omitempty"`
	UsageMetadata *GeminiUsageMetadata `json:"usageMetadata,omitempty"`
	ModelVersion  string               `json:"modelVersion,omitempty"`
}

// ParseGeminiNonStreamingResponse parses a complete JSON response from Gemini generateContent.
func ParseGeminiNonStreamingResponse(body []byte) (model string, usage GeminiUsageMetadata, err error) {
	var resp geminiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", GeminiUsageMetadata{}, err
	}
	if resp.UsageMetadata != nil {
		usage = *resp.UsageMetadata
	}
	return resp.ModelVersion, usage, nil
}

// ParseGeminiStreamChunk parses a single SSE data payload from a Gemini streaming response.
// Returns model, usage, whether usage was present, and whether content parts were present.
func ParseGeminiStreamChunk(data []byte) (model string, usage GeminiUsageMetadata, hasUsage bool, hasParts bool) {
	var resp geminiResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", GeminiUsageMetadata{}, false, false
	}
	model = resp.ModelVersion
	if resp.UsageMetadata != nil {
		usage = *resp.UsageMetadata
		hasUsage = true
	}
	if len(resp.Candidates) > 0 && resp.Candidates[0].Content != nil && len(resp.Candidates[0].Content.Parts) > 0 {
		hasParts = true
	}
	return model, usage, hasUsage, hasParts
}
