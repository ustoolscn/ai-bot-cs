package app

import "fmt"

type presetModel struct {
	Kind              string `json:"kind"`
	Model             string `json:"model"`
	Label             string `json:"label"`
	SupportsWebSearch bool   `json:"supportsWebSearch"`
}

type providerPreset struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	BaseURL       string        `json:"baseUrl"`
	WebSearchMode string        `json:"webSearchMode"`
	KeyURL        string        `json:"keyUrl"`
	Models        []presetModel `json:"models"`
}

var modelProviderPresets = []providerPreset{
	{"deepseek", "DeepSeek 官方", "https://api.deepseek.com", "deepseek", "https://platform.deepseek.com/api_keys", []presetModel{
		{"chat", "deepseek-flash", "DeepSeek Flash", true},
		{"chat", "deepseek-v4-pro", "DeepSeek V4 Pro", true},
	}},
	{"zhipu", "智谱 GLM 官方", "https://open.bigmodel.cn/api/paas/v4", "zhipu", "https://bigmodel.cn/usercenter/proj-mgmt/apikeys", []presetModel{
		{"chat", "glm-4.7", "GLM-4.7", true},
		{"chat", "glm-5.3", "GLM-5.3", true},
		{"chat", "glm-5.3-flash", "GLM-5.3-Flash", true},
	}},
}

func (in *modelInput) applyProvider() error {
	if in.Provider == "" {
		in.Provider = "compatible"
	}
	if in.Provider == "compatible" {
		switch in.WebSearchMode {
		case "", "disabled", "qwen", "openai", "responses", "custom":
			return nil
		default:
			return fmt.Errorf("联网搜索模式无效")
		}
	}
	for _, p := range modelProviderPresets {
		if p.ID != in.Provider {
			continue
		}
		if in.Kind == "" {
			in.Kind = "chat"
		}
		if in.Kind != "chat" {
			return fmt.Errorf("该服务商仅预置对话模型")
		}
		in.BaseURL = p.BaseURL
		if in.Model == "" {
			in.Model = p.Models[0].Model
		}
		if in.Name == "" {
			in.Name = p.Name + " · " + in.Model
		}
		if in.WebSearchMode != "disabled" {
			in.WebSearchMode = p.WebSearchMode
		}
		return nil
	}
	return fmt.Errorf("模型服务商无效")
}
