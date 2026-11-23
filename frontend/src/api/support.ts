import { request } from './client'
import type { AgentExample, LiveData, LiveDataSettings } from './agent'
import { mockLiveData } from '@/mock/agent'

/** 每个 AI 客服自己的设置。null 表示沿用（技能/工具：全部可用；知识库：跟随群绑定）。 */
export interface ServiceSettings {
  stylePrompt: string
  examples: AgentExample[]
  toolsEnabled: boolean
  maxToolSteps: number
  skillIds: string[] | null
  toolNames: string[] | null
  knowledgeBaseIds: string[] | null
  evaluationEnabled: boolean
  staffMiningEnabled: boolean
  autoPublishStaff: boolean
  autoPublishMinConfidence: number
  learningKnowledgeBaseId: string
  handoffPauseMinutes: number
  liveData: LiveDataSettings
}
export interface BotService {
  id: string
  name: string
  instructions: string
  chatProfileId: string
  /** 生图技能用的模型（模型页里 kind=image 的配置），空表示不能生图 */
  imageProfileId?: string
  simpleProfileId: string
  decisionProfileId: string
  settings: ServiceSettings
}

const mockService: BotService = {
  id: 'svc-demo', name: '网站 A 客服', instructions: '回答网站 A 的接入、价格和分组问题', chatProfileId: '', simpleProfileId: '', decisionProfileId: '',
  settings: {
    stylePrompt: '像熟练的真人客服一样说话，先给结论，一般 1～3 句话。', examples: [], toolsEnabled: true, maxToolSteps: 4, skillIds: null, toolNames: null, knowledgeBaseIds: null,
    evaluationEnabled: true, staffMiningEnabled: true, autoPublishStaff: false, autoPublishMinConfidence: 0.85, learningKnowledgeBaseId: '', handoffPauseMinutes: 30,
    liveData: { enabled: true, baseUrl: 'https://example.com', hasAccessToken: false, userId: '', injectSnapshot: true, priceNote: '', statusPageUrl: '' },
  },
}

export function botServices(botId: string) {
  const base = '/bots/' + botId + '/services'
  return {
    list: () => request<BotService[]>({ url: base }, [mockService]),
    save: (record: BotService, liveDataAccessToken?: string) => request<{ id: string }>({
      url: record.id ? base + '/' + record.id : base,
      method: record.id ? 'PUT' : 'POST',
      data: liveDataAccessToken === undefined ? record : { ...record, liveDataAccessToken },
    }),
    liveData: (id: string) => ({
      get: () => request<LiveData>({ url: `${base}/${id}/live-data` }, mockLiveData),
      refresh: () => request<LiveData>({ method: 'POST', url: `${base}/${id}/live-data/refresh`, timeout: 45000 }),
    }),
  }
}
