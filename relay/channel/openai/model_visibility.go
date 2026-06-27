package openai

import (
	"github.com/MAX-API-Next/MAX-API/common"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
)

func clientVisibleModelName(info *relaycommon.RelayInfo) string {
	return relaycommon.ClientVisibleModelName(info)
}

func outputModelName(info *relaycommon.RelayInfo) string {
	return relaycommon.OutputModelName(info)
}

func maskResponseModelBytes(info *relaycommon.RelayInfo, body []byte) ([]byte, error) {
	modelName := clientVisibleModelName(info)
	if modelName == "" || len(body) == 0 {
		return body, nil
	}
	var bodyMap map[string]any
	if err := common.Unmarshal(body, &bodyMap); err != nil {
		return nil, err
	}
	if _, ok := bodyMap["model"]; !ok {
		return body, nil
	}
	bodyMap["model"] = modelName
	return common.Marshal(bodyMap)
}

func maskChatStreamResponseModel(info *relaycommon.RelayInfo, data string) (string, error) {
	modelName := clientVisibleModelName(info)
	if modelName == "" || data == "" {
		return data, nil
	}
	var bodyMap map[string]any
	if err := common.UnmarshalJsonStr(data, &bodyMap); err != nil {
		return "", err
	}
	if _, ok := bodyMap["model"]; !ok {
		return data, nil
	}
	bodyMap["model"] = modelName
	masked, err := common.Marshal(bodyMap)
	if err != nil {
		return "", err
	}
	return string(masked), nil
}

func maskResponsesStreamResponseModel(info *relaycommon.RelayInfo, data string) (string, error) {
	modelName := clientVisibleModelName(info)
	if modelName == "" || data == "" {
		return data, nil
	}
	var bodyMap map[string]any
	if err := common.UnmarshalJsonStr(data, &bodyMap); err != nil {
		return "", err
	}
	response, ok := bodyMap["response"].(map[string]any)
	if !ok {
		return data, nil
	}
	if _, ok := response["model"]; !ok {
		return data, nil
	}
	response["model"] = modelName
	masked, err := common.Marshal(bodyMap)
	if err != nil {
		return "", err
	}
	return string(masked), nil
}
