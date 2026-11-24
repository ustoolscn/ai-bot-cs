import type { ModelProviderId, ModelProviderPreset, WebSearchMode } from '@/types'

/** 官方服务商预置。后端提供 /model-provider-presets 时以后端为准，这里是兜底。 */
export const modelProviderPresets: ModelProviderPreset[] = [
  {
    id: 'deepseek',
    name: 'DeepSeek 官方',
    baseUrl: 'https://api.deepseek.com',
    webSearchMode: 'deepseek',
    keyUrl: 'https://platform.deepseek.com/api_keys',
    models: [
      { kind: 'chat', model: 'deepseek-flash', label: 'DeepSeek Flash', supportsWebSearch: true },
      { kind: 'chat', model: 'deepseek-v4-pro', label: 'DeepSeek-V4 Pro', supportsWebSearch: true },
    ],
  },
  {
    id: 'zhipu',
    name: '智谱 GLM 官方',
    baseUrl: 'https://open.bigmodel.cn/api/paas/v4',
    webSearchMode: 'zhipu',
    keyUrl: 'https://bigmodel.cn/usercenter/proj-mgmt/apikeys',
    models: [
      { kind: 'chat', model: 'glm-4.7', label: 'GLM-4.7', supportsWebSearch: true },
      { kind: 'chat', model: 'glm-5.3', label: 'GLM-5.3', supportsWebSearch: true },
      { kind: 'chat', model: 'glm-5.3-flash', label: 'GLM-5.3 Flash', supportsWebSearch: true },
    ],
  },
  {
    id: 'typesafe',
    name: 'TypeSafe',
    baseUrl: 'https://api.typesafe.ai/v1',
    webSearchMode: 'disabled',
    keyUrl: 'https://console.typesafe.ai/home',
    models: [
      { kind: 'decision', model: 'jev-latest', label: 'Jev（最新）' },
      { kind: 'decision', model: 'jev-preview', label: 'Jev Preview' },
      { kind: 'decision', model: 'jev-1.14.0', label: 'Jev 1.14.0' },
      { kind: 'decision', model: 'jev-1.13.0', label: 'Jev 1.13.0' },
    ],
  },
]

export const providerLabel = (id: ModelProviderId | undefined, presets: ModelProviderPreset[] = modelProviderPresets) =>
  !id || id === 'compatible' ? '兼容接口' : presets.find((p) => p.id === id)?.name || id

/** 官方联网方式由服务商决定 */
export const officialSearchModes: WebSearchMode[] = ['deepseek', 'zhipu']
