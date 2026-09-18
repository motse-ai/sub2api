package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSelectAccountSkipsCompatStoreWhenPreviousResponseCannotMove(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(10190)
	official := Account{
		ID: 19101, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 10,
		Credentials: map[string]any{
			"api_key":  "sk-official",
			"base_url": "https://api.openai.com/v1",
		},
	}
	aiio := Account{
		ID: 19102, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0,
		Credentials: map[string]any{
			"api_key":  "sk-aiio",
			"base_url": "https://m.aiio.chat/v1",
			"model_mapping": map[string]any{
				"gpt-5.6-sol": "MiniMax-M3",
			},
		},
		Extra: map[string]any{"openai_responses_supported": true},
	}

	newSvc := func(accounts []Account) *OpenAIGatewayService {
		cfg := &config.Config{}
		cfg.Gateway.Scheduling.LoadBatchEnabled = false
		return &OpenAIGatewayService{
			accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
			cache:              &schedulerTestGatewayCache{},
			cfg:                cfg,
			concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
		}
	}

	t.Run("without previous_response_id prefers higher-priority compat account", func(t *testing.T) {
		svc := newSvc([]Account{official, aiio})
		selection, _, err := svc.SelectAccountWithSchedulerForCapability(
			ctx, &groupID, "", "", "gpt-5.6-sol", nil,
			OpenAIUpstreamTransportAny, "",
			false, false, true,
		)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.Equal(t, int64(19102), selection.Account.ID)
	})

	t.Run("sticky continuation stays on official gpt store", func(t *testing.T) {
		svc := newSvc([]Account{official, aiio})
		selection, _, err := svc.SelectAccountWithSchedulerForCapability(
			ctx, &groupID, "resp_openai", "", "gpt-5.6-sol", nil,
			OpenAIUpstreamTransportAny, "",
			false, false, true,
		)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.Equal(t, int64(19101), selection.Account.ID)
	})

	t.Run("does not failover continuation onto mapped MiniMax", func(t *testing.T) {
		svc := newSvc([]Account{official, aiio})
		selection, _, err := svc.SelectAccountWithSchedulerForCapability(
			ctx, &groupID, "resp_openai", "", "gpt-5.6-sol",
			map[int64]struct{}{19101: {}},
			OpenAIUpstreamTransportAny, "",
			false, false, true,
		)
		require.Error(t, err)
		require.Nil(t, selection)
	})
}
