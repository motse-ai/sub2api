//go:build unit

package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesStreamEncryptedReasoningFailureRetriesBeforeWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stream := "event: response.failed\n" +
		`data: {"type":"response.failed","response":{"id":"resp_eb5bf79eeeb34e47b7fde50db33208e9","object":"response","status":"failed","model":"gpt-5.6-sol","output":[],"error":{"code":"invalid_encrypted_content","type":"invalid_request_error","message":"The encrypted content MwIZ...6Nxs could not be verified. Reason: Encrypted content could not be decrypted or parsed."}}}` + "\n\n"
	tests := []struct {
		name string
		run  func(*OpenAIGatewayService, *gin.Context, *http.Response, *Account) error
	}{
		{
			name: "native",
			run: func(svc *OpenAIGatewayService, c *gin.Context, resp *http.Response, account *Account) error {
				_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "gpt-5.6-sol", "gpt-5.6-sol")
				return err
			},
		},
		{
			name: "passthrough",
			run: func(svc *OpenAIGatewayService, c *gin.Context, resp *http.Response, account *Account) error {
				_, err := svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, account, time.Now(), "gpt-5.6-sol", "gpt-5.6-sol")
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(stream)),
			}
			svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}
			err := tt.run(svc, c, resp, &Account{ID: 149, Platform: PlatformOpenAI, Type: AccountTypeOAuth})

			var encryptedErr *openAIEncryptedReasoningStreamError
			require.ErrorAs(t, err, &encryptedErr)
			require.Contains(t, encryptedErr.message, "could not be decrypted or parsed")
			require.NotContains(t, rec.Body.String(), "could not be decrypted or parsed")
			require.NotContains(t, rec.Body.String(), "MwIZ")
		})
	}
}

func TestResponsesStreamEncryptedFunctionOutputStillReachesClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stream := "event: error\n" +
		`data: {"type":"error","error":{"code":"invalid_encrypted_content","type":"invalid_request_error","message":"Encrypted function output content could not be decrypted or decoded."}}` + "\n\n"
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(stream)),
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}
	_, err := svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, time.Now(), "gpt-5.6-sol", "gpt-5.6-sol")

	var encryptedErr *openAIEncryptedReasoningStreamError
	require.NotErrorAs(t, err, &encryptedErr)
	require.Contains(t, rec.Body.String(), "could not be decrypted or decoded")
}
