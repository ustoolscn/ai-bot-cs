import { request } from './client'

export interface Personality { id: string; name: string; prompt: string }
export interface SupportAgent { id: string; name: string; instructions: string; personalityId: string }
export interface BotService { id: string; name: string; agentId: string; chatProfileId: string; simpleProfileId: string; decisionProfileId: string }

function collection<T extends { id: string }>(url: string) {
  return {
    list: () => request<T[]>({ url }),
    save: (record: T) => request<{ id: string }>({
      url: record.id ? url + '/' + record.id : url,
      method: record.id ? 'PUT' : 'POST',
      data: record,
    }),
  }
}

export const personalities = collection<Personality>('/personalities')
export const supportAgents = collection<SupportAgent>('/support-agents')
export const botServices = (botId: string) => collection<BotService>('/bots/' + botId + '/services')
