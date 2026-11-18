package sheets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultOpenRouterModel is used when no OpenRouter model is configured.
// OpenRouter model IDs use dots in the version ("claude-opus-5.5").
const DefaultOpenRouterModel = "anthropic/claude-haiku-5.5"

var openRouterURL = "https://openrouter.ai/api/v1/chat/completions"

// ParseOpenRouter extracts the connections through OpenRouter's
// OpenAI-compatible chat completions API, with a JSON-schema response format
// so the reply has the same shape as the Anthropic path.
// text is the CSV produced by aiRows.
func ParseOpenRouter(ctx context.Context, apiKey, model string, text string) ([]Entry, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("no OpenRouter API key set (Settings → AI)")
	}
	if model = strings.TrimSpace(model); model == "" {
		model = DefaultOpenRouterModel
	}
	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 32000,
		"messages": []map[string]string{
			{"role": "system", "content": aiSystem},
			{"role": "user", "content": "<sheet>\n" + text + "</sheet>"},
		},
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "ssh_sessions",
				"strict": true,
				"schema": aiSchema,
			},
		},
		// Only route to providers that honour response_format.
		"provider": map[string]any{"require_parameters": true},
	})

	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Title", "Pinion") // app name shown in the OpenRouter dashboard

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("AI parsing failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("AI parsing failed: %w", err)
	}

	var out struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &out)

	msg := ""
	if out.Error != nil {
		msg = out.Error.Message
	}
	switch {
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return nil, errors.New("the OpenRouter API key was rejected (Settings → AI)")
	case resp.StatusCode == 402:
		return nil, errors.New("OpenRouter: not enough credits on this key")
	case resp.StatusCode == 404 || (resp.StatusCode == 400 && strings.Contains(strings.ToLower(msg), "model")):
		return nil, fmt.Errorf("OpenRouter does not accept model %q (Settings → AI): %s", model, msg)
	case resp.StatusCode == 429:
		return nil, errors.New("OpenRouter rate limit reached; try again shortly")
	case resp.StatusCode != http.StatusOK:
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		return nil, fmt.Errorf("OpenRouter error (HTTP %d): %s", resp.StatusCode, msg)
	case out.Error != nil:
		// OpenRouter can report upstream failures inside a 200 response.
		return nil, fmt.Errorf("OpenRouter error: %s", msg)
	case len(out.Choices) == 0:
		return nil, errors.New("OpenRouter returned no answer")
	}

	c := out.Choices[0]
	switch {
	case c.Message.Refusal != "" || c.FinishReason == "content_filter":
		return nil, errors.New("the AI declined to read this sheet; turn AI off to use the built-in rules")
	case c.FinishReason == "length":
		return nil, errors.New("the sheet has too many rows for one AI request; turn AI off to use the built-in rules")
	}
	return decodeAI(c.Message.Content)
}
