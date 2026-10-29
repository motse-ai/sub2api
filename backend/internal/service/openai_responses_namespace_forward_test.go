package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// codexNamespaceRequestBody 模拟 Codex 多智能体请求：collaboration 命名空间声明 +
// 历史里的命名空间调用项 + 带残留 namespace 的普通消息项。
const codexNamespaceRequestBody = `{
	"model":"gpt-5.6-terra",
	"stream":false,
	"instructions":"test",
	"tools":[
		{"type":"namespace","name":"collaboration","description":"Tools for spawning and managing sub-agents.","tools":[
			{"type":"function","name":"spawn_agent","description":"Call as to=functions.collaboration.spawn_agent","parameters":{"type":"object"}},
			{"type":"function","name":"wait_agent","parameters":{"type":"object"}}
		]},
		{"type":"function","name":"exec","parameters":{"type":"object"}}
	],
	"input":[
		{"type":"function_call","namespace":"collaboration","name":"spawn_agent","call_id":"call_1","arguments":"{}"},
		{"type":"message","role":"user","namespace":"leftover","content":[{"type":"input_text","text":"hello"}]}
	]
}`

const namespaceForwardOKResponse = `{"id":"resp_ns","output":[],"usage":{"input_tokens":1,"output_tokens":1,"input_tokens_details":{"cached_tokens":0}}}`

// OAuth 出口即 namespace 扩展的定义方：声明必须原样送达，历史调用项必须保留
// namespace（缺字段上游会 400 "Missing namespace for function_call"），而非调用项上的
// 残留 namespace 仍要清掉。回归 issue #4978。
func TestOpenAIGatewayService_OAuthPreservesCodexNamespaceTools(t *testing.T) {
	body := []byte(codexNamespaceRequestBody)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(http.StatusOK, namespaceForwardOKResponse),
	}}
	c := newOpenAIRejectedFieldTestContext(body)

	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(), c, newOpenAIOAuthNamespaceTestAccount(), body,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	forwarded := upstream.bodies[0]

	namespaceTool := gjson.GetBytes(forwarded, `tools.#(type=="namespace")`)
	require.True(t, namespaceTool.Exists(), "namespace 声明必须原样转发")
	require.Equal(t, "collaboration", namespaceTool.Get("name").String())
	require.Equal(t, "spawn_agent", namespaceTool.Get("tools.0.name").String())
	require.Equal(t, "wait_agent", namespaceTool.Get("tools.1.name").String())
	// 摊平名一旦出现，模型就无法按工具描述里的 to=functions.collaboration.spawn_agent 寻址。
	require.NotContains(t, string(forwarded), "collaboration__spawn_agent")

	require.Equal(t, "collaboration", gjson.GetBytes(forwarded, "input.0.namespace").String())
	require.Equal(t, "spawn_agent", gjson.GetBytes(forwarded, "input.0.name").String())
	require.False(t, gjson.GetBytes(forwarded, "input.1.namespace").Exists())

	// 未摊平即无需回程还原，不得登记映射。
	require.Empty(t, openAIResponsesNamespaceNames(c))
}

// API Key 自定义上游若接受 namespace 工具声明，也要求历史 function_call 原样携带
// namespace。声明仍为命名空间工具却清掉调用项字段，会触发 Missing namespace。
func TestOpenAIGatewayService_APIKeyPreservesDeclaredNamespaceToolCalls(t *testing.T) {
	body := []byte(codexNamespaceRequestBody)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(http.StatusOK, namespaceForwardOKResponse),
	}}
	c := newOpenAIRejectedFieldTestContext(body)

	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(), c, newOpenAIRejectedFieldTestAccount(), body,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	forwarded := upstream.bodies[0]

	require.True(t, gjson.GetBytes(forwarded, `tools.#(type=="namespace")`).Exists())
	require.Equal(t, "collaboration", gjson.GetBytes(forwarded, "input.0.namespace").String())
	require.False(t, gjson.GetBytes(forwarded, "input.1.namespace").Exists())
}

// Responses Lite 把 namespace 声明放在 input.additional_tools，而不是顶层 tools。
// OpenAI API Key 走该请求形态时也必须保留历史 function_call.namespace。
func TestOpenAIGatewayService_APIKeyPreservesLiteDeclaredNamespaceToolCalls(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.6-terra",
		"stream":false,
		"input":[
			{"type":"function_call","namespace":"collaboration","name":"spawn_agent","call_id":"call_spawn","arguments":"{}"},
			{"type":"function_call","namespace":"mcp__cua_repl","name":"js","call_id":"call_js","arguments":"{}"},
			{"type":"message","role":"user","namespace":"leftover","content":[{"type":"input_text","text":"hello"}]},
			{"type":"additional_tools","role":"developer","tools":[
				{"type":"namespace","name":"collaboration","tools":[{"type":"function","name":"spawn_agent","parameters":{"type":"object"}}]},
				{"type":"namespace","name":"mcp__cua_repl","tools":[{"type":"function","name":"js","parameters":{"type":"object"}}]}
			]}
		]
	}`)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(http.StatusOK, namespaceForwardOKResponse),
	}}
	c := newOpenAIRejectedFieldTestContext(body)
	c.Request.Header.Set(responsesLiteHeader, "true")

	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(), c, newOpenAIRejectedFieldTestAccount(), body,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	forwarded := upstream.bodies[0]

	require.Equal(t, "collaboration", gjson.GetBytes(forwarded, `input.#(type=="additional_tools").tools.0.name`).String())
	require.Equal(t, "mcp__cua_repl", gjson.GetBytes(forwarded, `input.#(type=="additional_tools").tools.1.name`).String())
	require.Equal(t, "collaboration", gjson.GetBytes(forwarded, "input.0.namespace").String())
	require.Equal(t, "spawn_agent", gjson.GetBytes(forwarded, "input.0.name").String())
	require.Equal(t, "mcp__cua_repl", gjson.GetBytes(forwarded, "input.1.namespace").String())
	require.Equal(t, "js", gjson.GetBytes(forwarded, "input.1.name").String())
	require.False(t, gjson.GetBytes(forwarded, "input.2.namespace").Exists())
}

// Cursor 把 namespace 挂在 function 工具上，而不是 type=namespace 包裹。
// 历史 function_call 若被清掉 namespace，上游会按默认命名空间解析 read_thread 并 400。
func TestOpenAIGatewayService_APIKeyPreservesFunctionToolNamespaceField(t *testing.T) {
	body := []byte(`{
		"model":"gpt-6.1-sol",
		"stream":false,
		"tools":[
			{"type":"function","name":"read_thread","namespace":"cursor","parameters":{"type":"object"}},
			{"type":"function","name":"exec","parameters":{"type":"object"}}
		],
		"input":[
			{"type":"function_call","namespace":"cursor","name":"read_thread","call_id":"call_read","arguments":"{}"},
			{"type":"message","role":"user","namespace":"leftover","content":[{"type":"input_text","text":"hello"}]}
		]
	}`)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(http.StatusOK, namespaceForwardOKResponse),
	}}
	c := newOpenAIRejectedFieldTestContext(body)

	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(), c, newOpenAIRejectedFieldTestAccount(), body,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	forwarded := upstream.bodies[0]

	require.Equal(t, "cursor", gjson.GetBytes(forwarded, "tools.0.namespace").String())
	require.Equal(t, "read_thread", gjson.GetBytes(forwarded, "tools.0.name").String())
	require.Equal(t, "cursor", gjson.GetBytes(forwarded, "input.0.namespace").String())
	require.Equal(t, "read_thread", gjson.GetBytes(forwarded, "input.0.name").String())
	require.False(t, gjson.GetBytes(forwarded, "input.1.namespace").Exists())
}

// Cursor 压缩回放会丢掉 function_call.namespace，但 tools 里仍声明 read_thread 属于
// 某个 namespace。上游按默认命名空间解析这个名字就会 400，需要按声明补回字段。
func TestOpenAIGatewayService_APIKeyFillsMissingNamespaceFromToolDeclaration(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "namespace wrapper",
			body: `{
				"model":"gpt-6.1-sol",
				"stream":false,
				"tools":[{"type":"namespace","name":"cursor","tools":[
					{"type":"function","name":"read_thread","parameters":{"type":"object"}},
					{"type":"function","name":"exec","parameters":{"type":"object"}}
				]}],
				"input":[
					{"type":"function_call","name":"read_thread","call_id":"call_read","arguments":"{}"},
					{"type":"function_call","name":"exec","call_id":"call_exec","arguments":"{}"},
					{"type":"message","role":"user","namespace":"leftover","content":[{"type":"input_text","text":"hello"}]}
				]
			}`,
		},
		{
			name: "function namespace field",
			body: `{
				"model":"gpt-6.1-sol",
				"stream":false,
				"tools":[{"type":"function","name":"read_thread","namespace":"cursor","parameters":{"type":"object"}}],
				"input":[
					{"type":"function_call","name":"read_thread","call_id":"call_read","arguments":"{}"},
					{"type":"message","role":"user","namespace":"leftover","content":[{"type":"input_text","text":"hello"}]}
				]
			}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(tt.body)
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				newOpenAIRejectedFieldTestResponse(http.StatusOK, namespaceForwardOKResponse),
			}}
			c := newOpenAIRejectedFieldTestContext(body)

			result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
				context.Background(), c, newOpenAIRejectedFieldTestAccount(), body,
			)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, upstream.bodies, 1)
			forwarded := upstream.bodies[0]
			require.Equal(t, "cursor", gjson.GetBytes(forwarded, "input.0.namespace").String())
			require.Equal(t, "read_thread", gjson.GetBytes(forwarded, "input.0.name").String())
			require.False(t, gjson.GetBytes(forwarded, `input.#(type=="message").namespace`).Exists())
		})
	}
}

// Cursor 压缩后的请求不再带 namespace 工具声明，但 function_call 仍带着模型
// 原样输出的 namespace。AIHubMix 要求原样回传，标准 Responses 账号则继续删掉。
func TestOpenAIGatewayService_AIHubMixKeepsClientToolCallNamespaceWithoutDeclaration(t *testing.T) {
	body := []byte(`{
		"model":"gpt-6.1-sol",
		"stream":false,
		"tools":[{"type":"function","name":"exec_command","parameters":{"type":"object"}}],
		"input":[
			{"type":"function_call","name":"read_thread","namespace":"codex_app","call_id":"call_read","arguments":"{}"},
			{"type":"message","role":"user","namespace":"leftover","content":[{"type":"input_text","text":"hello"}]}
		]
	}`)
	account := newOpenAIRejectedFieldTestAccount()
	account.Credentials["base_url"] = "https://aihubmix.com"
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(http.StatusOK, namespaceForwardOKResponse),
	}}
	c := newOpenAIRejectedFieldTestContext(body)

	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(), c, account, body,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	forwarded := upstream.bodies[0]
	require.Equal(t, "codex_app", gjson.GetBytes(forwarded, "input.0.namespace").String())
	require.Equal(t, "read_thread", gjson.GetBytes(forwarded, "input.0.name").String())
	require.False(t, gjson.GetBytes(forwarded, "input.1.namespace").Exists())
}

// compact 端点 schema 更窄：input[].namespace 会 400 Unknown parameter（issue #4761），
// 且没有证据表明它接受 namespace 工具声明。compact 只做历史摘要、不需要模型寻址工具，
// 因此保持既有的摊平 + 全量清理行为，不随默认值翻转扩大风险面。
func TestOpenAIGatewayService_OAuthCompactKeepsFlattening(t *testing.T) {
	body := []byte(codexNamespaceRequestBody)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(http.StatusOK, namespaceForwardOKResponse),
	}}
	c := newOpenAIRejectedFieldTestContext(body)
	c.Request.URL.Path = "/v1/responses/compact"

	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(), c, newOpenAIOAuthNamespaceTestAccount(), body,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	forwarded := upstream.bodies[0]

	require.False(t, gjson.GetBytes(forwarded, "input.0.namespace").Exists())
	require.False(t, gjson.GetBytes(forwarded, "input.1.namespace").Exists())
	require.False(t, gjson.GetBytes(forwarded, `tools.#(type=="namespace")`).Exists())
	require.Equal(t, "collaboration__spawn_agent", gjson.GetBytes(forwarded, "input.0.name").String())
}

// 账号开关为不认识 namespace 的兼容上游保留退路：打开后恢复 0.1.166 的摊平行为。
func TestOpenAIGatewayService_OAuthFlattenFlagRestoresLegacyBehavior(t *testing.T) {
	body := []byte(codexNamespaceRequestBody)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(http.StatusOK, namespaceForwardOKResponse),
	}}
	c := newOpenAIRejectedFieldTestContext(body)
	account := newOpenAIOAuthNamespaceTestAccount()
	account.Extra = map[string]any{"openai_responses_flatten_namespaces": true}

	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(), c, account, body,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	forwarded := upstream.bodies[0]

	require.False(t, gjson.GetBytes(forwarded, `tools.#(type=="namespace")`).Exists())
	require.True(t, gjson.GetBytes(forwarded, `tools.#(name=="collaboration__spawn_agent")`).Exists())
	require.True(t, gjson.GetBytes(forwarded, `tools.#(name=="collaboration__wait_agent")`).Exists())
	// 摊平后调用项已改写成平名，不得再带 namespace。
	require.Equal(t, "collaboration__spawn_agent", gjson.GetBytes(forwarded, "input.0.name").String())
	require.False(t, gjson.GetBytes(forwarded, "input.0.namespace").Exists())
	require.False(t, gjson.GetBytes(forwarded, "input.1.namespace").Exists())

	names := openAIResponsesNamespaceNames(c)
	require.Equal(t,
		apicompat.ResponsesNamespaceName{Namespace: "collaboration", Name: "spawn_agent"},
		names["collaboration__spawn_agent"],
	)
}

// handler 的 failover 在同一个 *gin.Context 上重试下一个账号；保留 namespace 的账号
// 不得沿用上一个账号登记的摊平名映射做回程还原。
func TestOpenAIGatewayService_ForwardClearsStaleNamespaceNames(t *testing.T) {
	body := []byte(codexNamespaceRequestBody)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(http.StatusOK, namespaceForwardOKResponse),
	}}
	c := newOpenAIRejectedFieldTestContext(body)
	setOpenAIResponsesNamespaceNames(c, map[string]apicompat.ResponsesNamespaceName{
		"stale__tool": {Namespace: "stale", Name: "tool"},
	})

	_, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(), c, newOpenAIOAuthNamespaceTestAccount(), body,
	)

	require.NoError(t, err)
	require.Empty(t, openAIResponsesNamespaceNames(c))
}
