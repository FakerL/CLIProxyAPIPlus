package claude

import (
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	"github.com/tidwall/gjson"
)

func TestBuildClaudeResponseIncludesCacheUsage(t *testing.T) {
	out := BuildClaudeResponse("pong", nil, "claude-sonnet-4-6", usage.Detail{
		InputTokens:         13,
		OutputTokens:        4,
		CacheReadTokens:     22000,
		CacheCreationTokens: 31,
	}, "end_turn")

	if got := gjson.GetBytes(out, "usage.input_tokens").Int(); got != 13 {
		t.Fatalf("input_tokens = %d, want 13", got)
	}
	if got := gjson.GetBytes(out, "usage.cache_read_input_tokens").Int(); got != 22000 {
		t.Fatalf("cache_read_input_tokens = %d, want 22000", got)
	}
	if got := gjson.GetBytes(out, "usage.cache_creation_input_tokens").Int(); got != 31 {
		t.Fatalf("cache_creation_input_tokens = %d, want 31", got)
	}
}

func TestBuildClaudeMessageDeltaEventIncludesCacheUsage(t *testing.T) {
	out := BuildClaudeMessageDeltaEvent("end_turn", usage.Detail{
		InputTokens:         13,
		OutputTokens:        4,
		CacheReadTokens:     22000,
		CacheCreationTokens: 31,
	})

	_, dataRaw, ok := strings.Cut(string(out), "data: ")
	if !ok {
		t.Fatalf("event payload missing data line: %s", out)
	}
	data := []byte(strings.TrimSpace(dataRaw))
	if !gjson.ValidBytes(data) {
		t.Fatalf("event payload is not json: %s", out)
	}
	if got := gjson.GetBytes(data, "usage.cache_read_input_tokens").Int(); got != 22000 {
		t.Fatalf("cache_read_input_tokens = %d, want 22000", got)
	}
	if got := gjson.GetBytes(data, "usage.cache_creation_input_tokens").Int(); got != 31 {
		t.Fatalf("cache_creation_input_tokens = %d, want 31", got)
	}
}
