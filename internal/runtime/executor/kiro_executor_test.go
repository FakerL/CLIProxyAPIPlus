package executor

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	kiroauth "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/kiro"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	"github.com/tidwall/gjson"
)

func TestBuildKiroEndpointConfigs(t *testing.T) {
	tests := []struct {
		name           string
		region         string
		expectedURL    string
		expectedOrigin string
		expectedName   string
	}{
		{
			name:           "Empty region - defaults to us-east-1",
			region:         "",
			expectedURL:    "https://q.us-east-1.amazonaws.com/generateAssistantResponse",
			expectedOrigin: "AI_EDITOR",
			expectedName:   "AmazonQ",
		},
		{
			name:           "us-east-1",
			region:         "us-east-1",
			expectedURL:    "https://q.us-east-1.amazonaws.com/generateAssistantResponse",
			expectedOrigin: "AI_EDITOR",
			expectedName:   "AmazonQ",
		},
		{
			name:           "ap-southeast-1",
			region:         "ap-southeast-1",
			expectedURL:    "https://q.ap-southeast-1.amazonaws.com/generateAssistantResponse",
			expectedOrigin: "AI_EDITOR",
			expectedName:   "AmazonQ",
		},
		{
			name:           "eu-west-1",
			region:         "eu-west-1",
			expectedURL:    "https://q.eu-west-1.amazonaws.com/generateAssistantResponse",
			expectedOrigin: "AI_EDITOR",
			expectedName:   "AmazonQ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configs := buildKiroEndpointConfigs(tt.region)

			if len(configs) != 2 {
				t.Fatalf("expected 2 endpoint configs, got %d", len(configs))
			}

			// Check primary endpoint (AmazonQ)
			primary := configs[0]
			if primary.URL != tt.expectedURL {
				t.Errorf("primary URL = %q, want %q", primary.URL, tt.expectedURL)
			}
			if primary.Origin != tt.expectedOrigin {
				t.Errorf("primary Origin = %q, want %q", primary.Origin, tt.expectedOrigin)
			}
			if primary.Name != tt.expectedName {
				t.Errorf("primary Name = %q, want %q", primary.Name, tt.expectedName)
			}
			if primary.AmzTarget != "" {
				t.Errorf("primary AmzTarget should be empty, got %q", primary.AmzTarget)
			}

			// Check fallback endpoint (CodeWhisperer)
			fallback := configs[1]
			if fallback.Name != "CodeWhisperer" {
				t.Errorf("fallback Name = %q, want %q", fallback.Name, "CodeWhisperer")
			}
			// CodeWhisperer fallback uses the same region as Q endpoint
			expectedRegion := tt.region
			if expectedRegion == "" {
				expectedRegion = kiroDefaultRegion
			}
			expectedFallbackURL := fmt.Sprintf("https://codewhisperer.%s.amazonaws.com/generateAssistantResponse", expectedRegion)
			if fallback.URL != expectedFallbackURL {
				t.Errorf("fallback URL = %q, want %q", fallback.URL, expectedFallbackURL)
			}
			if fallback.AmzTarget == "" {
				t.Error("fallback AmzTarget should NOT be empty")
			}
		})
	}
}

func TestGetKiroEndpointConfigs_NilAuth(t *testing.T) {
	configs := getKiroEndpointConfigs(nil)

	if len(configs) != 2 {
		t.Fatalf("expected 2 endpoint configs, got %d", len(configs))
	}

	// Should return default us-east-1 configs
	if configs[0].Name != "AmazonQ" {
		t.Errorf("first config Name = %q, want %q", configs[0].Name, "AmazonQ")
	}
	expectedURL := "https://q.us-east-1.amazonaws.com/generateAssistantResponse"
	if configs[0].URL != expectedURL {
		t.Errorf("first config URL = %q, want %q", configs[0].URL, expectedURL)
	}
}

func TestGetKiroEndpointConfigs_WithRegionFromProfileArn(t *testing.T) {
	auth := &cliproxyauth.Auth{
		Metadata: map[string]any{
			"profile_arn": "arn:aws:codewhisperer:ap-southeast-1:123456789012:profile/ABC",
		},
	}

	configs := getKiroEndpointConfigs(auth)

	if len(configs) != 2 {
		t.Fatalf("expected 2 endpoint configs, got %d", len(configs))
	}

	expectedURL := "https://q.ap-southeast-1.amazonaws.com/generateAssistantResponse"
	if configs[0].URL != expectedURL {
		t.Errorf("primary URL = %q, want %q", configs[0].URL, expectedURL)
	}
}

func TestGetKiroEndpointConfigs_WithApiRegionOverride(t *testing.T) {
	auth := &cliproxyauth.Auth{
		Metadata: map[string]any{
			"api_region":  "eu-central-1",
			"profile_arn": "arn:aws:codewhisperer:us-east-1:123456789012:profile/ABC",
		},
	}

	configs := getKiroEndpointConfigs(auth)

	// api_region should take precedence over profile_arn
	expectedURL := "https://q.eu-central-1.amazonaws.com/generateAssistantResponse"
	if configs[0].URL != expectedURL {
		t.Errorf("primary URL = %q, want %q", configs[0].URL, expectedURL)
	}
}

func TestGetKiroEndpointConfigs_PreferredEndpoint(t *testing.T) {
	tests := []struct {
		name              string
		preference        string
		expectedFirstName string
	}{
		{
			name:              "Prefer codewhisperer",
			preference:        "codewhisperer",
			expectedFirstName: "CodeWhisperer",
		},
		{
			name:              "Prefer ide (alias for codewhisperer)",
			preference:        "ide",
			expectedFirstName: "CodeWhisperer",
		},
		{
			name:              "Prefer amazonq",
			preference:        "amazonq",
			expectedFirstName: "AmazonQ",
		},
		{
			name:              "Prefer q (alias for amazonq)",
			preference:        "q",
			expectedFirstName: "AmazonQ",
		},
		{
			name:              "Prefer cli (alias for amazonq)",
			preference:        "cli",
			expectedFirstName: "AmazonQ",
		},
		{
			name:              "Unknown preference - no reordering",
			preference:        "unknown",
			expectedFirstName: "AmazonQ",
		},
		{
			name:              "Empty preference - no reordering",
			preference:        "",
			expectedFirstName: "AmazonQ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := &cliproxyauth.Auth{
				Metadata: map[string]any{
					"preferred_endpoint": tt.preference,
				},
			}

			configs := getKiroEndpointConfigs(auth)

			if configs[0].Name != tt.expectedFirstName {
				t.Errorf("first endpoint Name = %q, want %q", configs[0].Name, tt.expectedFirstName)
			}
		})
	}
}

func TestGetKiroEndpointConfigs_PreferredEndpointFromAttributes(t *testing.T) {
	// Test that preferred_endpoint can also come from Attributes
	auth := &cliproxyauth.Auth{
		Metadata:   map[string]any{},
		Attributes: map[string]string{"preferred_endpoint": "codewhisperer"},
	}

	configs := getKiroEndpointConfigs(auth)

	if configs[0].Name != "CodeWhisperer" {
		t.Errorf("first endpoint Name = %q, want %q", configs[0].Name, "CodeWhisperer")
	}
}

func TestGetKiroEndpointConfigs_MetadataTakesPrecedenceOverAttributes(t *testing.T) {
	auth := &cliproxyauth.Auth{
		Metadata:   map[string]any{"preferred_endpoint": "amazonq"},
		Attributes: map[string]string{"preferred_endpoint": "codewhisperer"},
	}

	configs := getKiroEndpointConfigs(auth)

	// Metadata should take precedence
	if configs[0].Name != "AmazonQ" {
		t.Errorf("first endpoint Name = %q, want %q", configs[0].Name, "AmazonQ")
	}
}

func TestGetAuthValue(t *testing.T) {
	tests := []struct {
		name     string
		auth     *cliproxyauth.Auth
		key      string
		expected string
	}{
		{
			name: "From metadata",
			auth: &cliproxyauth.Auth{
				Metadata: map[string]any{"test_key": "metadata_value"},
			},
			key:      "test_key",
			expected: "metadata_value",
		},
		{
			name: "From attributes (fallback)",
			auth: &cliproxyauth.Auth{
				Attributes: map[string]string{"test_key": "attribute_value"},
			},
			key:      "test_key",
			expected: "attribute_value",
		},
		{
			name: "Metadata takes precedence",
			auth: &cliproxyauth.Auth{
				Metadata:   map[string]any{"test_key": "metadata_value"},
				Attributes: map[string]string{"test_key": "attribute_value"},
			},
			key:      "test_key",
			expected: "metadata_value",
		},
		{
			name: "Key not found",
			auth: &cliproxyauth.Auth{
				Metadata:   map[string]any{"other_key": "value"},
				Attributes: map[string]string{"another_key": "value"},
			},
			key:      "test_key",
			expected: "",
		},
		{
			name: "Nil metadata",
			auth: &cliproxyauth.Auth{
				Attributes: map[string]string{"test_key": "attribute_value"},
			},
			key:      "test_key",
			expected: "attribute_value",
		},
		{
			name:     "Both nil",
			auth:     &cliproxyauth.Auth{},
			key:      "test_key",
			expected: "",
		},
		{
			name: "Value is trimmed and lowercased",
			auth: &cliproxyauth.Auth{
				Metadata: map[string]any{"test_key": "  UPPER_VALUE  "},
			},
			key:      "test_key",
			expected: "upper_value",
		},
		{
			name: "Empty string value in metadata - falls back to attributes",
			auth: &cliproxyauth.Auth{
				Metadata:   map[string]any{"test_key": ""},
				Attributes: map[string]string{"test_key": "attribute_value"},
			},
			key:      "test_key",
			expected: "attribute_value",
		},
		{
			name: "Non-string value in metadata - falls back to attributes",
			auth: &cliproxyauth.Auth{
				Metadata:   map[string]any{"test_key": 123},
				Attributes: map[string]string{"test_key": "attribute_value"},
			},
			key:      "test_key",
			expected: "attribute_value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getAuthValue(tt.auth, tt.key)
			if result != tt.expected {
				t.Errorf("getAuthValue() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGetAccountKey(t *testing.T) {
	tests := []struct {
		name    string
		auth    *cliproxyauth.Auth
		checkFn func(t *testing.T, result string)
	}{
		{
			name: "From client_id",
			auth: &cliproxyauth.Auth{
				Metadata: map[string]any{
					"client_id":     "test-client-id-123",
					"refresh_token": "test-refresh-token-456",
				},
			},
			checkFn: func(t *testing.T, result string) {
				expected := kiroauth.GetAccountKey("test-client-id-123", "test-refresh-token-456")
				if result != expected {
					t.Errorf("expected %s, got %s", expected, result)
				}
			},
		},
		{
			name: "From refresh_token only",
			auth: &cliproxyauth.Auth{
				Metadata: map[string]any{
					"refresh_token": "test-refresh-token-789",
				},
			},
			checkFn: func(t *testing.T, result string) {
				expected := kiroauth.GetAccountKey("", "test-refresh-token-789")
				if result != expected {
					t.Errorf("expected %s, got %s", expected, result)
				}
			},
		},
		{
			name: "Nil auth",
			auth: nil,
			checkFn: func(t *testing.T, result string) {
				if len(result) != 16 {
					t.Errorf("expected 16 char key, got %d chars", len(result))
				}
			},
		},
		{
			name: "Nil metadata",
			auth: &cliproxyauth.Auth{},
			checkFn: func(t *testing.T, result string) {
				if len(result) != 16 {
					t.Errorf("expected 16 char key, got %d chars", len(result))
				}
			},
		},
		{
			name: "Empty metadata",
			auth: &cliproxyauth.Auth{
				Metadata: map[string]any{},
			},
			checkFn: func(t *testing.T, result string) {
				if len(result) != 16 {
					t.Errorf("expected 16 char key, got %d chars", len(result))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getAccountKey(tt.auth)
			tt.checkFn(t, result)
		})
	}
}

func TestEndpointAliases(t *testing.T) {
	// Verify all expected aliases are defined
	expectedAliases := map[string]string{
		"codewhisperer": "codewhisperer",
		"ide":           "codewhisperer",
		"amazonq":       "amazonq",
		"q":             "amazonq",
		"cli":           "amazonq",
	}

	for alias, target := range expectedAliases {
		if actual, ok := endpointAliases[alias]; !ok {
			t.Errorf("missing alias %q", alias)
		} else if actual != target {
			t.Errorf("alias %q = %q, want %q", alias, actual, target)
		}
	}

	// Verify no unexpected aliases
	if len(endpointAliases) != len(expectedAliases) {
		t.Errorf("unexpected number of aliases: got %d, want %d", len(endpointAliases), len(expectedAliases))
	}
}

func TestMapModelToKiro_MapsClaudeOpus47Variants(t *testing.T) {
	executor := &KiroExecutor{}
	tests := []struct {
		name     string
		model    string
		expected string
	}{
		{
			name:     "kiro alias",
			model:    "kiro-claude-opus-4-7",
			expected: "claude-opus-4.7",
		},
		{
			name:     "kiro agentic alias",
			model:    "kiro-claude-opus-4-7-agentic",
			expected: "claude-opus-4.7",
		},
		{
			name:     "native hyphen alias",
			model:    "claude-opus-4-7",
			expected: "claude-opus-4.7",
		},
		{
			name:     "native dotted alias",
			model:    "claude-opus-4.7",
			expected: "claude-opus-4.7",
		},
		{
			name:     "native agentic alias",
			model:    "claude-opus-4.7-agentic",
			expected: "claude-opus-4.7",
		},
		{
			name:     "dated alias collapses to canonical version",
			model:    "claude-sonnet-4-5-20250929",
			expected: "claude-sonnet-4.5",
		},
		{
			name:     "amazonq prefix",
			model:    "amazonq-claude-sonnet-4-5",
			expected: "claude-sonnet-4.5",
		},
		{
			name:     "claude opus 4.5 hyphenated alias",
			model:    "kiro-claude-opus-4-5",
			expected: "claude-opus-4.5",
		},
		{
			name:     "claude haiku 4.5 hyphenated alias",
			model:    "kiro-claude-haiku-4-5",
			expected: "claude-haiku-4.5",
		},
		{
			name:     "claude sonnet 4.6 hyphenated alias",
			model:    "kiro-claude-sonnet-4-6",
			expected: "claude-sonnet-4.6",
		},
		{
			name:     "minimax m2.1 hyphenated alias",
			model:    "kiro-minimax-m2-1",
			expected: "minimax-m2.1",
		},
		{
			name:     "non-Claude model passes through",
			model:    "kiro-glm-5",
			expected: "glm-5",
		},
		{
			name:     "non-Claude minimax versioned",
			model:    "kiro-minimax-m2-5",
			expected: "minimax-m2.5",
		},
		{
			name:     "gpt version with trailing codename (terra)",
			model:    "kiro-gpt-5-6-terra",
			expected: "gpt-5.6-terra",
		},
		{
			name:     "canonical gpt version remains unchanged",
			model:    "gpt-5.6-terra",
			expected: "gpt-5.6-terra",
		},
		{
			name:     "gpt version with trailing codename (sol)",
			model:    "kiro-gpt-5-6-sol",
			expected: "gpt-5.6-sol",
		},
		{
			name:     "gpt version with trailing codename (luna)",
			model:    "kiro-gpt-5-6-luna",
			expected: "gpt-5.6-luna",
		},
		{
			name:     "kimi version with trailing codename",
			model:    "kiro-kimi-k2-7-code",
			expected: "kimi-k2.7-code",
		},
		{
			name:     "canonical kimi version remains unchanged",
			model:    "kimi-k2.7-code",
			expected: "kimi-k2.7-code",
		},
		{
			name:     "deepseek versioned",
			model:    "kiro-deepseek-3-2",
			expected: "deepseek-3.2",
		},
		{
			name:     "canonical deepseek version remains unchanged",
			model:    "deepseek-3.2",
			expected: "deepseek-3.2",
		},
		{
			name:     "grok version keeps trailing build segment",
			model:    "kiro-grok-4-20-0309-reasoning",
			expected: "grok-4.20-0309-reasoning",
		},
		{
			name:     "canonical grok version remains unchanged",
			model:    "grok-4.20-0309-reasoning",
			expected: "grok-4.20-0309-reasoning",
		},
		{
			name:     "single digit segment left alone",
			model:    "kiro-claude-sonnet-5",
			expected: "claude-sonnet-5",
		},
		{
			name:     "identifier without trailing version unchanged",
			model:    "kiro-qwen3-coder-next",
			expected: "qwen3-coder-next",
		},
		{
			name:     "unknown model passes through unchanged",
			model:    "kiro-future-model-9",
			expected: "future-model-9",
		},
		{
			name:     "unknown date-like numeric ID passes through unchanged",
			model:    "kiro-model-2024-05-preview",
			expected: "model-2024-05-preview",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := executor.mapModelToKiro(tt.model)
			if got != tt.expected {
				t.Fatalf("mapModelToKiro(%q) = %q, want %q", tt.model, got, tt.expected)
			}
			if normalizedAgain := executor.mapModelToKiro(got); normalizedAgain != got {
				t.Fatalf("mapModelToKiro() is not idempotent: first = %q, second = %q", got, normalizedAgain)
			}
		})
	}
}

func TestApplyKiroTokenUsagePreservesCacheTokenBreakdown(t *testing.T) {
	detail := usage.Detail{}
	ok := applyKiroTokenUsage(&detail, map[string]interface{}{
		"uncachedInputTokens":   float64(290),
		"outputTokens":          float64(1),
		"totalTokens":           float64(4130),
		"cacheReadInputTokens":  float64(3822),
		"cacheWriteInputTokens": float64(17),
	})
	if !ok {
		t.Fatal("applyKiroTokenUsage() = false, want true")
	}
	if detail.InputTokens != 290 {
		t.Fatalf("InputTokens = %d, want 290", detail.InputTokens)
	}
	if detail.OutputTokens != 1 {
		t.Fatalf("OutputTokens = %d, want 1", detail.OutputTokens)
	}
	if detail.TotalTokens != 4130 {
		t.Fatalf("TotalTokens = %d, want 4130", detail.TotalTokens)
	}
	if detail.CacheReadTokens != 3822 {
		t.Fatalf("CacheReadTokens = %d, want 3822", detail.CacheReadTokens)
	}
	if detail.CacheCreationTokens != 17 {
		t.Fatalf("CacheCreationTokens = %d, want 17", detail.CacheCreationTokens)
	}
	if detail.CachedTokens != 3822 {
		t.Fatalf("CachedTokens = %d, want 3822", detail.CachedTokens)
	}
}

func TestFinalizeKiroUsageTotalIncludesCacheTokensWhenUpstreamTotalMissing(t *testing.T) {
	detail := usage.Detail{
		InputTokens:         290,
		OutputTokens:        1,
		CacheReadTokens:     3822,
		CacheCreationTokens: 17,
	}
	finalizeKiroUsageTotal(&detail)
	if detail.TotalTokens != 4130 {
		t.Fatalf("TotalTokens = %d, want 4130", detail.TotalTokens)
	}
}

func TestKiroContextUsageFallbackDoesNotOverwritePreciseTokenUsage(t *testing.T) {
	detail := usage.Detail{}
	hasPreciseTokenUsage := applyKiroTokenUsage(&detail, map[string]interface{}{
		"uncachedInputTokens":    float64(290),
		"outputTokens":           float64(1),
		"totalTokens":            float64(4130),
		"cacheReadInputTokens":   float64(3822),
		"cacheWriteInputTokens":  float64(17),
		"contextUsagePercentage": float64(50),
	})
	if !hasPreciseTokenUsage {
		t.Fatal("applyKiroTokenUsage() = false, want true")
	}
	if _, applied := applyKiroContextUsageFallback(&detail, 50, hasPreciseTokenUsage); applied {
		t.Fatal("applyKiroContextUsageFallback() applied despite precise token usage")
	}
	if detail.InputTokens != 290 {
		t.Fatalf("InputTokens = %d, want precise uncached value 290", detail.InputTokens)
	}
	if detail.TotalTokens != 4130 {
		t.Fatalf("TotalTokens = %d, want upstream total 4130", detail.TotalTokens)
	}
}

func TestKiroContextUsageFallbackAppliesWhenPreciseTokenUsageMissing(t *testing.T) {
	detail := usage.Detail{OutputTokens: 3}
	calculated, applied := applyKiroContextUsageFallback(&detail, 50, false)
	if !applied {
		t.Fatal("applyKiroContextUsageFallback() applied = false, want true")
	}
	if calculated != 100000 {
		t.Fatalf("calculated input tokens = %d, want 100000", calculated)
	}
	if detail.InputTokens != 100000 {
		t.Fatalf("InputTokens = %d, want 100000", detail.InputTokens)
	}
	if detail.TotalTokens != 100003 {
		t.Fatalf("TotalTokens = %d, want 100003", detail.TotalTokens)
	}
}

func TestParseEventStreamTokenUsagePreservesKiroCacheFields(t *testing.T) {
	executor := &KiroExecutor{}
	stream := bytes.NewBuffer(nil)
	stream.Write(kiroEventStreamFrame("assistantResponseEvent", []byte(`{"assistantResponseEvent":{"content":"pong"}}`)))
	stream.Write(kiroEventStreamFrame("messageMetadataEvent", []byte(`{"messageMetadataEvent":{"tokenUsage":{"uncachedInputTokens":13,"cacheReadInputTokens":22000,"cacheWriteInputTokens":31,"outputTokens":4,"totalTokens":22048,"contextUsagePercentage":3.61}}}`)))

	content, _, usageInfo, _, billingSignals, err := executor.parseEventStream(stream)
	if err != nil {
		t.Fatalf("parseEventStream() error = %v", err)
	}
	if !billingSignals.HasTokenUsage {
		t.Fatal("expected tokenUsage billing signal")
	}
	if content != "pong" {
		t.Fatalf("content = %q, want %q", content, "pong")
	}
	if usageInfo.InputTokens != 13 {
		t.Fatalf("input tokens = %d, want uncached input tokens 13", usageInfo.InputTokens)
	}
	if usageInfo.CacheReadTokens != 22000 {
		t.Fatalf("cache read tokens = %d, want 22000", usageInfo.CacheReadTokens)
	}
	if usageInfo.CacheCreationTokens != 31 {
		t.Fatalf("cache creation tokens = %d, want 31", usageInfo.CacheCreationTokens)
	}
	if usageInfo.CachedTokens != 22000 {
		t.Fatalf("cached tokens = %d, want 22000", usageInfo.CachedTokens)
	}
	if usageInfo.OutputTokens != 4 {
		t.Fatalf("output tokens = %d, want 4", usageInfo.OutputTokens)
	}
	if usageInfo.TotalTokens != 22048 {
		t.Fatalf("total tokens = %d, want upstream total 22048", usageInfo.TotalTokens)
	}
}

func TestParseEventStreamWithoutTokenUsageOnlyCapturesBillingSignals(t *testing.T) {
	executor := &KiroExecutor{}
	stream := bytes.NewBuffer(nil)
	stream.Write(kiroEventStreamFrame("assistantResponseEvent", []byte(`{"assistantResponseEvent":{"content":"pong"}}`)))
	stream.Write(kiroEventStreamFrame("contextUsageEvent", []byte(`{"contextUsageEvent":{"contextUsagePercentage":19.332000732421875}}`)))
	stream.Write(kiroEventStreamFrame("meteringEvent", []byte(`{"meteringEvent":{"unit":"credit","usage":0.08355490150912107}}`)))

	_, _, usageInfo, _, billingSignals, err := executor.parseEventStream(stream)
	if err != nil {
		t.Fatalf("parseEventStream() error = %v", err)
	}
	if billingSignals.HasTokenUsage {
		t.Fatal("did not expect tokenUsage billing signal")
	}
	if !billingSignals.HasCreditUsage {
		t.Fatal("expected credit usage billing signal")
	}
	if usageInfo.CacheReadTokens != 0 || usageInfo.CacheCreationTokens != 0 {
		t.Fatalf("parseEventStream should not add missing Kiro cache fields, got read=%d create=%d", usageInfo.CacheReadTokens, usageInfo.CacheCreationTokens)
	}
	if usageInfo.InputTokens != 0 {
		t.Fatalf("parseEventStream should leave input tokens unset before billing inference, got %d", usageInfo.InputTokens)
	}
}

func TestInferKiroBillingUsageFirstWrite(t *testing.T) {
	got, ok := inferKiroBillingUsage("claude-sonnet-4.5", usage.Detail{OutputTokens: 1}, kiroBillingSignals{
		ContextPercentage: 19.332000732421875,
		CreditUsage:       0.15851445374792705,
		HasCreditUsage:    true,
	})
	if !ok {
		t.Fatal("expected billing usage inference")
	}
	if got.InputTokens != 38664 {
		t.Fatalf("input tokens = %d, want 38664", got.InputTokens)
	}
	if got.CacheReadTokens != 0 {
		t.Fatalf("cache read tokens = %d, want 0", got.CacheReadTokens)
	}
	if got.OutputTokens != 1 {
		t.Fatalf("output tokens = %d, want 1", got.OutputTokens)
	}
}

func TestInferKiroBillingUsageCacheRead(t *testing.T) {
	got, ok := inferKiroBillingUsage("claude-sonnet-4.5", usage.Detail{OutputTokens: 1}, kiroBillingSignals{
		ContextPercentage: 19.332000732421875,
		CreditUsage:       0.08355490150912107,
		HasCreditUsage:    true,
	})
	if !ok {
		t.Fatal("expected billing usage inference")
	}
	if got.InputTokens != 0 {
		t.Fatalf("input tokens = %d, want 0", got.InputTokens)
	}
	if got.CacheReadTokens != 38664 {
		t.Fatalf("cache read tokens = %d, want 38664", got.CacheReadTokens)
	}
	if got.CachedTokens != 38664 {
		t.Fatalf("cached tokens = %d, want 38664", got.CachedTokens)
	}
}

func TestInferKiroBillingUsageOpus48CacheRead(t *testing.T) {
	got, ok := inferKiroBillingUsage("kiro-claude-opus-4-8", usage.Detail{OutputTokens: 1}, kiroBillingSignals{
		ContextPercentage: 1.8154001235961914,
		CreditUsage:       0.06725680490878938,
		HasCreditUsage:    true,
	})
	if !ok {
		t.Fatal("expected billing usage inference")
	}
	if got.InputTokens != 0 {
		t.Fatalf("input tokens = %d, want 0", got.InputTokens)
	}
	if got.CacheReadTokens != 3631 {
		t.Fatalf("cache read tokens = %d, want 3631", got.CacheReadTokens)
	}
	if got.OutputTokens != 1 {
		t.Fatalf("output tokens = %d, want 1", got.OutputTokens)
	}
}

func TestInferKiroBillingUsageSonnet5CacheRead(t *testing.T) {
	got, ok := inferKiroBillingUsage("kiro-claude-sonnet-5", usage.Detail{OutputTokens: 1}, kiroBillingSignals{
		ContextPercentage: 3.0002999305725098,
		CreditUsage:       0.06528776524046434,
		HasCreditUsage:    true,
	})
	if !ok {
		t.Fatal("expected billing usage inference")
	}
	if got.InputTokens != 0 {
		t.Fatalf("input tokens = %d, want 0", got.InputTokens)
	}
	if got.CacheReadTokens != 6001 {
		t.Fatalf("cache read tokens = %d, want 6001", got.CacheReadTokens)
	}
	if got.OutputTokens != 1 {
		t.Fatalf("output tokens = %d, want 1", got.OutputTokens)
	}
}

func TestInferKiroBillingUsageOpus5CacheRead(t *testing.T) {
	got, ok := inferKiroBillingUsage("kiro-claude-opus-5", usage.Detail{OutputTokens: 1}, kiroBillingSignals{
		ContextPercentage: 3.0288000106811523,
		CreditUsage:       0.11159245996683251,
		HasCreditUsage:    true,
	})
	if !ok {
		t.Fatal("expected billing usage inference")
	}
	if got.InputTokens != 0 {
		t.Fatalf("input tokens = %d, want 0", got.InputTokens)
	}
	if got.CacheReadTokens != 6058 {
		t.Fatalf("cache read tokens = %d, want 6058", got.CacheReadTokens)
	}
	if got.OutputTokens != 1 {
		t.Fatalf("output tokens = %d, want 1", got.OutputTokens)
	}
}

func TestInferKiroBillingUsageInfersOutputWhenUncached(t *testing.T) {
	rates, ok := kiroBillingRatesForModel("claude-haiku-4.5")
	if !ok {
		t.Fatal("missing haiku rates")
	}
	contextPercentage := 5.0
	contextTokens := int64(10000)
	credits := float64(contextTokens)*rates.InputPerToken + 42*rates.OutputPerToken

	got, ok := inferKiroBillingUsage("claude-haiku-4.5", usage.Detail{}, kiroBillingSignals{
		ContextPercentage: contextPercentage,
		CreditUsage:       credits,
		HasCreditUsage:    true,
	})
	if !ok {
		t.Fatal("expected billing usage inference")
	}
	if got.InputTokens != contextTokens {
		t.Fatalf("input tokens = %d, want %d", got.InputTokens, contextTokens)
	}
	if got.OutputTokens != 42 {
		t.Fatalf("output tokens = %d, want 42", got.OutputTokens)
	}
	if got.CacheReadTokens != 0 {
		t.Fatalf("cache read tokens = %d, want 0", got.CacheReadTokens)
	}
}

func TestKiroBillingRatesMatchClientModelAliases(t *testing.T) {
	for _, model := range []string{
		"kiro-gpt-5-6-sol",
		"kiro-gpt-5-6-terra",
		"kiro-gpt-5-6-luna",
		"gpt-5.6-sol",
		"gpt-5.6-terra",
		"gpt-5.6-luna",
		"kiro-claude-sonnet-4-5",
		"kiro-claude-haiku-4-5",
		"kiro-claude-sonnet-5",
		"kiro-claude-sonnet-4-6",
		"kiro-claude-opus-4-6",
		"kiro-claude-opus-4-7",
		"kiro-claude-opus-4-8",
		"kiro-claude-opus-5",
		"claude-sonnet-4-5-20250929",
		"claude-haiku-4-5-20251001",
		"claude-sonnet-5",
		"claude-opus-4-8",
		"claude-opus-5",
	} {
		t.Run(model, func(t *testing.T) {
			if _, ok := kiroBillingRatesForModel(model); !ok {
				t.Fatalf("expected billing rates for %s", model)
			}
		})
	}
}

func TestKiroGPT56BillingRates(t *testing.T) {
	tests := []struct {
		model  string
		input  float64
		cache  float64
		output float64
	}{
		{model: "gpt-5.6-sol", input: 0.01028458, cache: 0.00542413, output: 0.26515303},
		{model: "gpt-5.6-terra", input: 0.00428524, cache: 0.00226005, output: 0.11048043},
		{model: "gpt-5.6-luna", input: 0.00042852, cache: 0.00022601, output: 0.01104804},
	}

	for _, test := range tests {
		t.Run(test.model, func(t *testing.T) {
			rates, ok := kiroBillingRatesForModel(test.model)
			if !ok {
				t.Fatal("expected billing rates")
			}
			if math.Abs(rates.InputPerToken*1000-test.input) > 1e-12 || math.Abs(rates.CachePerToken*1000-test.cache) > 1e-12 || math.Abs(rates.OutputPerToken*1000-test.output) > 1e-12 {
				t.Fatalf("rates per 1000 tokens = input %.8f cache %.8f output %.8f", rates.InputPerToken*1000, rates.CachePerToken*1000, rates.OutputPerToken*1000)
			}
		})
	}

	if _, ok := kiroBillingRatesForModel("gpt-5.6-solar"); ok {
		t.Fatal("did not expect a partial GPT-5.6 model-name match")
	}
}

func TestInferKiroGPT56BillingUsageCacheRead(t *testing.T) {
	for _, model := range []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"} {
		t.Run(model, func(t *testing.T) {
			rates, ok := kiroBillingRatesForModel(model)
			if !ok {
				t.Fatal("expected billing rates")
			}
			const contextTokens int64 = 10000
			credits := float64(contextTokens)*rates.CachePerToken + rates.OutputPerToken
			got, inferred := inferKiroBillingUsage(model, usage.Detail{OutputTokens: 1}, kiroBillingSignals{
				ContextPercentage: 5,
				CreditUsage:       credits,
				HasCreditUsage:    true,
			})
			if !inferred {
				t.Fatal("expected billing usage inference")
			}
			if got.InputTokens != 0 || got.CacheReadTokens != contextTokens || got.OutputTokens != 1 {
				t.Fatalf("usage = input %d cache %d output %d", got.InputTokens, got.CacheReadTokens, got.OutputTokens)
			}
		})
	}
}

func TestInjectKiroGPT56ReasoningEffort(t *testing.T) {
	for _, model := range []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"} {
		for _, effort := range []string{"low", "medium", "high", "xhigh"} {
			t.Run(model+"/"+effort, func(t *testing.T) {
				got, injected := injectKiroGPT56ReasoningEffort(
					[]byte(`{"conversationState":{}}`),
					[]byte(`{"reasoning":{"effort":"`+effort+`"}}`),
					model,
				)
				if !injected {
					t.Fatal("expected effort injection")
				}
				if actual := gjson.GetBytes(got, "additionalModelRequestFields.reasoning.effort").String(); actual != effort {
					t.Fatalf("reasoning effort = %q, want %q; payload=%s", actual, effort, got)
				}
			})
		}
	}
}

func TestInjectKiroGPT56ReasoningEffortChatField(t *testing.T) {
	got, injected := injectKiroGPT56ReasoningEffort(
		[]byte(`{"conversationState":{}}`),
		[]byte(`{"reasoning_effort":"high"}`),
		"gpt-5.6-terra",
	)
	if !injected || gjson.GetBytes(got, "additionalModelRequestFields.reasoning.effort").String() != "high" {
		t.Fatalf("expected chat reasoning_effort injection; payload=%s", got)
	}
}

func TestInjectKiroGPT56ReasoningEffortOmitsUnsupportedValuesAndModels(t *testing.T) {
	tests := []struct {
		name    string
		model   string
		request string
	}{
		{name: "invalid effort", model: "gpt-5.6-sol", request: `{"reasoning":{"effort":"max"}}`},
		{name: "non GPT-5.6 model", model: "claude-opus-5", request: `{"reasoning":{"effort":"high"}}`},
		{name: "missing effort", model: "gpt-5.6-luna", request: `{}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, injected := injectKiroGPT56ReasoningEffort([]byte(`{"conversationState":{}}`), []byte(test.request), test.model)
			if injected {
				t.Fatalf("did not expect effort injection; payload=%s", got)
			}
			if gjson.GetBytes(got, "additionalModelRequestFields.reasoning.effort").Exists() {
				t.Fatalf("unexpected effort field; payload=%s", got)
			}
		})
	}
}

func kiroEventStreamFrame(eventType string, payload []byte) []byte {
	headerName := ":event-type"
	headers := make([]byte, 0, 1+len(headerName)+1+2+len(eventType))
	headers = append(headers, byte(len(headerName)))
	headers = append(headers, headerName...)
	headers = append(headers, 7)
	headers = binary.BigEndian.AppendUint16(headers, uint16(len(eventType)))
	headers = append(headers, eventType...)

	totalLength := uint32(12 + len(headers) + len(payload) + 4)
	frame := make([]byte, 12, totalLength)
	binary.BigEndian.PutUint32(frame[0:4], totalLength)
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(headers)))
	frame = append(frame, headers...)
	frame = append(frame, payload...)
	frame = binary.BigEndian.AppendUint32(frame, 0)
	return frame
}
