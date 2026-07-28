package moderation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// TextResult is one text-layer verdict.
type TextResult struct {
	Verdict    Verdict
	Categories []string
	Language   string
	Reason     string
}

// TextModerator classifies caption and hashtag text.
//
// An interface because the implementation is a paid external call on the
// pre-publish path: tests must not make it, and an outage must degrade to
// review rather than to approve.
type TextModerator interface {
	Name() string
	ModerateText(ctx context.Context, text string) (TextResult, error)
}

// ClaudeModerator is the §3.3 implementation.
//
// Why a language model here and nowhere else in the pipeline: captions in this
// market are Gulf-dialect Arabic, transliterated Arabic in Latin script with
// digits for letters ("7abibi"), or Arabic and English code-switched inside one
// sentence. Comprehend's toxicity detection is weak on Arabic and effectively
// blind to all three. Image and video moderation stay with Rekognition, where a
// specialised vision model is both better and an order of magnitude cheaper.
type ClaudeModerator struct {
	client anthropic.Client
	model  anthropic.Model
}

type ClaudeConfig struct {
	APIKey string
	// Model defaults to Opus 5. Haiku 4.5 is the obvious cost lever at
	// moderation volume — roughly 5× cheaper — but note that its minimum
	// cacheable prefix is 4096 tokens against Opus's 512. Switching models
	// without checking that the policy prompt still crosses the line turns
	// caching off silently and costs more than the switch saves.
	Model string
}

func NewClaudeModerator(cfg ClaudeConfig) (*ClaudeModerator, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("moderation: anthropic api key is required")
	}
	model := anthropic.Model(cfg.Model)
	if model == "" {
		model = anthropic.ModelClaudeOpus5
	}
	return &ClaudeModerator{
		client: anthropic.NewClient(option.WithAPIKey(cfg.APIKey)),
		model:  model,
	}, nil
}

func (m *ClaudeModerator) Name() string { return "claude" }

func (m *ClaudeModerator) ModerateText(ctx context.Context, text string) (TextResult, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		// Nothing to classify. Approving an empty caption is correct — the
		// image and video layers still run, and a reel with no words is not a
		// text-policy problem.
		return TextResult{Verdict: VerdictApprove, Language: "und"}, nil
	}

	resp, err := m.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     m.model,
		MaxTokens: 512,
		System: []anthropic.TextBlockParam{{
			Text: policyPrompt,
			// The cache breakpoint. The policy is identical on every call, so
			// cached reads bill at roughly a tenth of input price — which is
			// the difference between text moderation being a rounding error
			// against the CDN bill and being comparable to it (§7).
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}},
		OutputConfig: anthropic.OutputConfigParam{
			// Classification, not reasoning. Low effort rather than disabled
			// thinking: disabling it outright has known failure modes on Opus 5,
			// and low already captures most of the saving.
			Effort: anthropic.OutputConfigEffortLow,
			Format: anthropic.JSONOutputFormatParam{Schema: moderationSchema()},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(text)),
		},
	})
	if err != nil {
		return TextResult{}, fmt.Errorf("moderation: claude call: %w", err)
	}

	raw := firstText(resp)
	if raw == "" {
		return TextResult{}, fmt.Errorf("moderation: claude returned no text block")
	}

	var out struct {
		Verdict    string   `json:"verdict"`
		Categories []string `json:"categories"`
		Language   string   `json:"language"`
		Reason     string   `json:"reason"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return TextResult{}, fmt.Errorf("moderation: decode claude verdict: %w", err)
	}

	verdict, ok := parseVerdict(out.Verdict)
	if !ok {
		// The schema constrains this, so reaching here means something changed
		// underneath us. Fail loudly rather than defaulting — a moderation
		// component that guesses is worse than one that is down, because the
		// guess is invisible.
		return TextResult{}, fmt.Errorf("moderation: unexpected verdict %q", out.Verdict)
	}

	return TextResult{
		Verdict:    verdict,
		Categories: out.Categories,
		Language:   out.Language,
		Reason:     out.Reason,
	}, nil
}

func firstText(m *anthropic.Message) string {
	if m == nil {
		return ""
	}
	// Iterate rather than indexing [0]: with adaptive thinking on, the first
	// block is a thinking block and the JSON verdict is the one after it.
	for _, block := range m.Content {
		if block.Type == "text" {
			return block.AsText().Text
		}
	}
	return ""
}

func parseVerdict(s string) (Verdict, bool) {
	switch Verdict(strings.ToLower(strings.TrimSpace(s))) {
	case VerdictApprove:
		return VerdictApprove, true
	case VerdictReview:
		return VerdictReview, true
	case VerdictReject:
		return VerdictReject, true
	}
	return "", false
}

// StubTextModerator approves everything. It exists so the pipeline runs without
// an API key in development and in tests; cmd/worker refuses it in production.
type StubTextModerator struct{}

func (StubTextModerator) Name() string { return "stub" }

func (StubTextModerator) ModerateText(context.Context, string) (TextResult, error) {
	return TextResult{Verdict: VerdictApprove, Language: "und", Reason: "stub moderator"}, nil
}
