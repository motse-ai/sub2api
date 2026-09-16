package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestShouldDisableOpenAICompatContinuationForAnthropicIngress(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	t.Run("nil_context", func(t *testing.T) {
		t.Parallel()
		require.False(t, shouldDisableOpenAICompatContinuationForAnthropicIngress(nil))
	})

	t.Run("direct_v1_messages", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
		c.Request.Header.Set("User-Agent", "claude-cli/2.1.220 (external, cli)")
		require.False(t, shouldDisableOpenAICompatContinuationForAnthropicIngress(c))
	})

	t.Run("marked_anthropic_ingress", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)
		MarkAnthropicCompatIngress(c)
		require.True(t, shouldDisableOpenAICompatContinuationForAnthropicIngress(c))
	})
}
