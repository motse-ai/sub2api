//go:build unit

package service

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
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

func TestStripEncryptedFunctionOutputKeepsPlaintextAndCallID(t *testing.T) {
	body := []byte(`{"model":"gpt-6.1-sol","input":[` +
		`{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"},` +
		`{"type":"function_call_output","call_id":"call_1","output":"visible result","encrypted_content":"cipher"},` +
		`{"type":"function_call_output","call_id":"call_cipher_only","encrypted_content":"cipher-only"}` +
		`]}`)

	stripped, changed, err := stripOpenAIEncryptedFunctionOutputs(body)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "function_call_output", gjson.GetBytes(stripped, "input.1.type").String())
	require.Equal(t, "call_1", gjson.GetBytes(stripped, "input.1.call_id").String())
	require.Equal(t, "visible result", gjson.GetBytes(stripped, "input.1.output").String())
	require.False(t, gjson.GetBytes(stripped, "input.1.encrypted_content").Exists())
	require.Equal(t, 2, int(gjson.GetBytes(stripped, "input.#").Int()), "ciphertext without plaintext cannot be replayed")

	_, changed, err = stripOpenAIEncryptedFunctionOutputs(stripped)
	require.NoError(t, err)
	require.False(t, changed)
}

func TestResponsesStreamEncryptedFunctionOutputRetriesBeforeWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader("event: error\n" +
				`data: {"type":"error","error":{"code":"invalid_encrypted_content","message":"Encrypted function output content could not be decrypted or decoded."}}` + "\n\n")),
		},
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader("event: response.output_text.delta\n" +
				`data: {"type":"response.output_text.delta","delta":"ok"}` + "\n\n" +
				"event: response.completed\n" +
				`data: {"type":"response.completed","response":{"id":"resp_retry","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}` + "\n\n")),
		},
	}}
	body := []byte(`{"model":"gpt-6.1-sol","stream":true,"input":[{"type":"function_call_output","call_id":"call_1","output":"visible result","encrypted_content":"cipher"}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID:       244,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://api.openai.com/v1",
		},
		Status:      StatusActive,
		Schedulable: true,
	}

	_, err := svc.forwardOpenAIPassthrough(c.Request.Context(), c, account, body, body, "gpt-6.1-sol", false, nil, true, time.Now())
	require.NoError(t, err)
	require.Len(t, upstream.bodies, 2)
	require.False(t, gjson.GetBytes(upstream.bodies[1], "input.0.encrypted_content").Exists())
	require.Equal(t, "visible result", gjson.GetBytes(upstream.bodies[1], "input.0.output").String())
	require.Equal(t, "call_1", gjson.GetBytes(upstream.bodies[1], "input.0.call_id").String())
	require.NotContains(t, rec.Body.String(), "could not be decrypted")
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

	require.Error(t, err)
	require.Contains(t, err.Error(), "could not be decrypted or decoded")
}
