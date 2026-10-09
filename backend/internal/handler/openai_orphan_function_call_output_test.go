//go:build unit

package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeOrphanFunctionCallOutputsRewritesStandaloneResult(t *testing.T) {
	body := []byte(`{
		"model":"gpt-6.1-sol",
		"stream":true,
		"input":[
			{"type":"message","role":"user","content":"SECRET_PROMPT"},
			{"type":"function_call_output","name":"shell","namespace":"codex","output":"SECRET_OUTPUT"},
			{"type":"function_call_output","namespace":"mcp","output":[{"type":"output_text","text":"SECRET_PART"}]}
		]
	}`)

	got, changed := normalizeOrphanFunctionCallOutputs(body)
	require.True(t, changed)
	require.Equal(t, "message", gjson.GetBytes(got, "input.0.type").String())
	require.Equal(t, "user", gjson.GetBytes(got, "input.0.role").String())
	require.Equal(t, "message", gjson.GetBytes(got, "input.1.type").String())
	require.Equal(t, "developer", gjson.GetBytes(got, "input.1.role").String())
	require.Equal(t, "codex/shell:\nSECRET_OUTPUT", gjson.GetBytes(got, "input.1.content.0.text").String())
	require.Equal(t, "developer", gjson.GetBytes(got, "input.2.role").String())
	require.Equal(t, "mcp:\nSECRET_PART", gjson.GetBytes(got, "input.2.content.0.text").String())
	require.False(t, gjson.GetBytes(got, `input.#(type=="function_call_output")`).Exists())
	require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(got, "model").String())
}

func TestNormalizeOrphanFunctionCallOutputsLeavesPairedResults(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "call id present",
			body: `{"model":"gpt-5","input":[{"type":"function_call_output","call_id":"call-1","name":"shell","output":"ok"}]}`,
		},
		{
			name: "matching tool call",
			body: `{"model":"gpt-5","input":[{"type":"function_call","call_id":"call-1","name":"shell"},{"type":"function_call_output","name":"shell","output":"ok"}]}`,
		},
		{
			name: "previous response id",
			body: `{"model":"gpt-5","previous_response_id":"resp_1","input":[{"type":"function_call_output","name":"shell","output":"ok"}]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(tt.body)
			got, changed := normalizeOrphanFunctionCallOutputs(body)
			require.False(t, changed)
			require.Equal(t, body, got)
		})
	}
}
