package sheets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

// AI providers.
const (
	ProviderAnthropic  = "anthropic"
	ProviderOpenRouter = "openrouter"
)

// DefaultModel is used when no Anthropic model is configured. Measured on a
// 206-row site sheet (25-row chunks): Claude Haiku 5.5 found 220–221 of 221
// ports for about $0.025, Sonnet 5.5 221 for about $0.25, Opus 5.5 221 for
// about $0.39 (whole sheet). Rows are re-analysed only when they change.
const DefaultModel = "claude-haiku-5-5"

// maxAIInput keeps the request well inside the model's context window. A sheet
// larger than this is refused rather than silently cut off.
const maxAIInput = 600_000 // bytes of CSV

const aiSystem = `You read rows of a spreadsheet, given as CSV, and list the SSH connections they describe, for import into an SSH client.

The first column "#" is the row number in the original sheet, and the last column "context" may name the site of the row above. The first CSV line is the sheet's header row when it is known. Only some rows of the sheet may be included.

Return one entry per SSH port:
- row: the "#" value of the row the port is in.
- name: a session name taken from the sheet (site, device, server, person or label column; join a multi-line cell into one line).
- folder: a grouping column such as group or region, using "/" for nesting. Empty if the sheet has no such column.
- host: the hostname or IP address to connect to. Empty when the sheet lists only ports (they are all reached through one relay server the user supplies).
- port: the SSH port as an integer.
- user: the login user name if stated, else empty.

Rules:
- Read the headers in any language (e.g. 名稱, 主機, 位址, 埠, SSH port, 帳號, 案場). Use the SSH port column, not web or other port columns.
- A cell often holds several ports: "2222, 3333", "7100/7200", one per line, or labelled like "TR: 6810" / "4ch:7940" / "7550 小電腦" / "6230 (i5 VM)". Return EVERY port as its own entry. When a row has several ports or a label, append it to the name: "Site A (TR 6810)", "Site A (2222)".
- "6010改6000" or "改 8580 (原8460)" means the port changed: use only the new one (6000, 8580).
- Bracketed notes such as "(old:1.2.3.4:50222)", "(原8460)" or "(跟鴻匯衝)" are not ports. "ID:9130" is not a port.
- Take SSH ports only from the SSH port column. Numbers in other columns are not SSH ports: IP addresses, phone numbers, remote-desktop IDs, model numbers, web ports, and ports mentioned in notes (e.g. "SIP port 7033", "6183: HSC300"), even when a note says a port changed.
- A row with ports but an empty name belongs to the site above it (see the "context" column): use that site name followed by the port.
- Skip rows without an SSH port and rows that are notes, totals or headings.
- Go through every row given, to the last one; do not stop early or summarise.
- Never invent hosts, ports or users that are not in the sheet.
- Do not return passwords or other secrets, even if the sheet contains them.`

var aiSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"sessions": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":   map[string]any{"type": "string"},
					"folder": map[string]any{"type": "string"},
					"host":   map[string]any{"type": "string"},
					"port":   map[string]any{"type": "integer"},
					"user":   map[string]any{"type": "string"},
					"row":    map[string]any{"type": "integer"},
				},
				"required":             []string{"row", "name", "folder", "host", "port", "user"},
				"additionalProperties": false,
			},
		},
	},
	"required":             []string{"sessions"},
	"additionalProperties": false,
}

// ParseAI asks Claude to extract the connections from the rows.
// text is the CSV produced by aiRows.
func ParseAI(ctx context.Context, apiKey, model string, text string) ([]Entry, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("no Anthropic API key set (Settings → AI)")
	}
	if model = strings.TrimSpace(model); model == "" {
		model = DefaultModel
	}
	client := anthropic.NewClient(option.WithAPIKey(apiKey), option.WithMaxRetries(2))
	params := anthropic.BetaMessageNewParams{
		Model:     model,
		MaxTokens: 64000,
		System:    []anthropic.BetaTextBlockParam{{Text: aiSystem}},
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("<sheet>\n" + text + "</sheet>")),
		},
		// Effort is left at the model's default, which is what was measured.
		OutputConfig: anthropic.BetaOutputConfigParam{
			Format: anthropic.BetaJSONOutputFormatParam{Schema: aiSchema},
		},
	}
	if hasServerFallback(model) {
		// On a policy refusal, let the API re-serve the request on a fallback model.
		params.Fallbacks = anthropic.BetaFallbacksParamOfDefault()
		params.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
	}
	stream := client.Beta.Messages.NewStreaming(ctx, params)
	msg := anthropic.BetaMessage{}
	for stream.Next() {
		if err := msg.Accumulate(stream.Current()); err != nil {
			return nil, fmt.Errorf("AI parsing failed: %w", err)
		}
	}
	if err := stream.Err(); err != nil {
		var apierr *anthropic.Error
		if errors.As(err, &apierr) {
			switch apierr.StatusCode {
			case 401, 403:
				return nil, errors.New("the Anthropic API key was rejected (Settings → AI)")
			case 404:
				return nil, fmt.Errorf("unknown Claude model %q (Settings → AI)", model)
			case 429:
				return nil, errors.New("Claude API rate limit reached; try again shortly")
			}
		}
		return nil, fmt.Errorf("AI parsing failed: %w", err)
	}
	switch msg.StopReason {
	case anthropic.BetaStopReasonRefusal:
		return nil, errors.New("the AI declined to read this sheet; turn AI off to use the built-in rules")
	case anthropic.BetaStopReasonMaxTokens:
		return nil, errors.New("the sheet has too many rows for one AI request; turn AI off to use the built-in rules")
	}
	var out strings.Builder
	for _, b := range msg.Content {
		if t, ok := b.AsAny().(anthropic.BetaTextBlock); ok {
			out.WriteString(t.Text)
		}
	}
	return decodeAI(out.String())
}

// decodeAI reads the {"sessions": [...]} JSON the model returns.
func decodeAI(out string) ([]Entry, error) {
	var parsed struct {
		Sessions []Entry `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &parsed); err != nil {
		return nil, fmt.Errorf("AI returned unreadable output: %w", err)
	}
	return parsed.Sessions, nil
}

// hasServerFallback reports whether the model supports server-side refusal
// fallbacks (Claude Haiku does not).
func hasServerFallback(model string) bool {
	for _, p := range []string{"claude-opus-5", "claude-sonnet-5-5", "claude-fable-5", "claude-mythos-5"} {
		if strings.HasPrefix(model, p) {
			return true
		}
	}
	return false
}
