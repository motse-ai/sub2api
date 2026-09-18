package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShouldKeepOpenAIResponsesPreviousResponseID(t *testing.T) {
	officialGPT := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://api.openai.com/v1",
		},
	}
	officialEmptyBase := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "sk-test",
		},
	}
	aiioUnmapped := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-aiio",
			"base_url": "https://m.aiio.chat/v1",
		},
	}
	aiioMappedMiniMax := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-aiio",
			"base_url": "https://m.aiio.chat/v1",
			"model_mapping": map[string]any{
				"gpt-5.6-sol": "MiniMax-M3",
			},
		},
	}
	officialMappedMiniMax := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://api.openai.com/v1",
			"model_mapping": map[string]any{
				"gpt-5.6-sol": "MiniMax-M3",
			},
		},
	}
	oauthGPT := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	}

	tests := []struct {
		name          string
		account       *Account
		upstreamModel string
		want          bool
	}{
		{name: "official api key gpt model keeps", account: officialGPT, upstreamModel: "gpt-5.6-sol", want: true},
		{name: "official empty base defaults to api.openai.com", account: officialEmptyBase, upstreamModel: "gpt-5.1", want: true},
		{name: "official o-series keeps", account: officialGPT, upstreamModel: "o3-mini", want: true},
		{name: "official fine-tune gpt keeps", account: officialGPT, upstreamModel: "ft:gpt-4o-mini:org:id", want: true},
		{name: "official codex alias without gpt substring keeps", account: officialGPT, upstreamModel: "codex-auto-review", want: true},
		{name: "oauth gpt keeps", account: oauthGPT, upstreamModel: "gpt-5.3-codex", want: true},
		{name: "aiio unmapped gpt alias strips", account: aiioUnmapped, upstreamModel: "gpt-5.6-sol", want: false},
		{name: "aiio mapped minimax strips", account: aiioMappedMiniMax, upstreamModel: "MiniMax-M3", want: false},
		{name: "official mapped minimax strips", account: officialMappedMiniMax, upstreamModel: "MiniMax-M3", want: false},
		{name: "official grok strips", account: officialGPT, upstreamModel: "grok-4.5", want: false},
		{name: "nil account strips", account: nil, upstreamModel: "gpt-5.6-sol", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, shouldKeepOpenAIResponsesPreviousResponseID(tt.account, tt.upstreamModel))
		})
	}
}

func TestAccountSupportsOpenAIPreviousResponseContinuationUsesMappedUpstreamModel(t *testing.T) {
	aiio := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-aiio",
			"base_url": "https://m.aiio.chat/v1",
			"model_mapping": map[string]any{
				"gpt-5.6-sol": "MiniMax-M3",
			},
		},
	}
	official := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://api.openai.com/v1",
		},
	}

	require.True(t, accountSupportsOpenAIPreviousResponseContinuation(official, "gpt-5.6-sol", false, "", false),
		"no previous_response_id should not filter")
	require.True(t, accountSupportsOpenAIPreviousResponseContinuation(aiio, "gpt-5.6-sol", false, "resp_1", true),
		"self-contained continuation may move to compat accounts after the field is stripped")
	require.True(t, accountSupportsOpenAIPreviousResponseContinuation(official, "gpt-5.6-sol", false, "resp_1", false))
	require.False(t, accountSupportsOpenAIPreviousResponseContinuation(aiio, "gpt-5.6-sol", false, "resp_1", false),
		"must inspect mapped MiniMax-M3, not the gpt-5.6-sol request alias")
}
