package openai

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	"github.com/tidwall/gjson"
)

func TestBuildOpenAIResponseIncludesCachedPromptTokens(t *testing.T) {
	out := BuildOpenAIResponseWithReasoning("pong", "", nil, "claude-sonnet-4-6", usage.Detail{
		InputTokens:         13,
		OutputTokens:        4,
		CacheReadTokens:     22000,
		CacheCreationTokens: 31,
		TotalTokens:         22048,
	}, "end_turn")

	if got := gjson.GetBytes(out, "usage.prompt_tokens").Int(); got != 22044 {
		t.Fatalf("prompt_tokens = %d, want 22044", got)
	}
	if got := gjson.GetBytes(out, "usage.prompt_tokens_details.cached_tokens").Int(); got != 22000 {
		t.Fatalf("cached_tokens = %d, want 22000", got)
	}
	if got := gjson.GetBytes(out, "usage.total_tokens").Int(); got != 22048 {
		t.Fatalf("total_tokens = %d, want 22048", got)
	}
}

func TestConvertKiroNonStreamToOpenAIIncludesCachedPromptTokens(t *testing.T) {
	raw := []byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"pong"}],"stop_reason":"end_turn","usage":{"input_tokens":13,"output_tokens":4,"cache_read_input_tokens":22000,"cache_creation_input_tokens":31}}`)
	out := ConvertKiroNonStreamToOpenAI(nil, "claude-sonnet-4-6", nil, nil, raw, nil)

	if got := gjson.GetBytes(out, "usage.prompt_tokens").Int(); got != 22044 {
		t.Fatalf("prompt_tokens = %d, want 22044", got)
	}
	if got := gjson.GetBytes(out, "usage.prompt_tokens_details.cached_tokens").Int(); got != 22000 {
		t.Fatalf("cached_tokens = %d, want 22000", got)
	}
	if got := gjson.GetBytes(out, "usage.total_tokens").Int(); got != 22048 {
		t.Fatalf("total_tokens = %d, want 22048", got)
	}
}
