package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func mappedRelayInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: "public-model",
		ClientModelName: "public-model",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "private-upstream-model",
			IsModelMapped:     true,
		},
	}
}

func TestMaskResponseModelBytesUsesClientModelForMappedRequests(t *testing.T) {
	body := []byte(`{"id":"chatcmpl-test","object":"chat.completion","model":"private-upstream-model","choices":[]}`)

	masked, err := maskResponseModelBytes(mappedRelayInfo(), body)
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, common.Unmarshal(masked, &out))
	require.Equal(t, "public-model", out["model"])
	require.Equal(t, "chatcmpl-test", out["id"])
}

func TestMaskResponseModelBytesKeepsUnmappedRequests(t *testing.T) {
	body := []byte(`{"model":"private-upstream-model"}`)
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-model",
		ClientModelName: "public-model",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "private-upstream-model",
			IsModelMapped:     false,
		},
	}

	masked, err := maskResponseModelBytes(info, body)
	require.NoError(t, err)
	require.JSONEq(t, string(body), string(masked))
}

func TestMaskChatStreamResponseModelUsesClientModel(t *testing.T) {
	data := `{"id":"chunk-test","object":"chat.completion.chunk","model":"private-upstream-model","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`

	masked, err := maskChatStreamResponseModel(mappedRelayInfo(), data)
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, common.UnmarshalJsonStr(masked, &out))
	require.Equal(t, "public-model", out["model"])
	require.Equal(t, "chunk-test", out["id"])
}

func TestMaskResponsesStreamResponseModelUsesClientModel(t *testing.T) {
	data := `{"type":"response.completed","response":{"id":"resp-test","model":"private-upstream-model","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`

	masked, err := maskResponsesStreamResponseModel(mappedRelayInfo(), data)
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, common.UnmarshalJsonStr(masked, &out))
	response := out["response"].(map[string]any)
	require.Equal(t, "public-model", response["model"])
	require.Equal(t, "resp-test", response["id"])
}

func TestOpenaiHandlerMasksMappedModelInClientResponse(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	body := `{"id":"chatcmpl-test","object":"chat.completion","created":1710000000,"model":"private-upstream-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}

	usage, err := OpenaiHandler(c, mappedRelayInfo(), resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 2, usage.TotalTokens)
	require.Contains(t, recorder.Body.String(), `"model":"public-model"`)
	require.NotContains(t, recorder.Body.String(), "private-upstream-model")
}
