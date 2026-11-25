import { markRaw, type Component } from 'vue'
import { ChatDotRound, ChatLineSquare, CircleClose, Clock, Collection, Connection, Cpu, DataAnalysis, Headset, Monitor, Odometer, Reading, Setting } from '@element-plus/icons-vue'

export interface NavItem { path: string; label: string; description: string; icon: Component; badge?: 'handoffs' | 'candidates'; keywords?: string }
export interface NavGroup { title: string; items: NavItem[] }

const icon = (value: Component) => markRaw(value)

// 顺序跟着日常工作：先处理待办，再管群，再调客服，最后是知识、记录和系统。
export const navGroups: NavGroup[] = [
  { title: '工作台', items: [
    { path: '/overview', label: '今日概览', description: '待办、核心指标和服务健康', icon: icon(Odometer) },
    { path: '/handoffs', label: '人工台', description: '转人工的用户，在这里直接回复', icon: icon(Headset), badge: 'handoffs', keywords: '转人工 回复 结单' },
  ] },
  { title: 'QQ 群', items: [
    { path: '/conversations', label: '群与会话', description: '像 QQ 一样直接在群里聊天，接待设置和群管理在设置里', icon: icon(ChatDotRound), keywords: '成员 入群 审批 禁言 发言审核 发送 群管理 客服学习' },
    { path: '/schedules', label: '定时任务', description: '每天定时向群里发送内容', icon: icon(Clock), keywords: '定时 发送 天气 日报' },
    { path: '/blacklist', label: '黑名单', description: '拉黑的群成员，机器人不理睬并可自动踢出', icon: icon(CircleClose), keywords: '拉黑 踢人' },
  ] },
  { title: 'AI 客服', items: [
    { path: '/bots', label: '机器人与客服', description: 'QQ 机器人接入和每个 AI 客服的人设、网站、工具', icon: icon(Connection), keywords: 'AppID 回调 AI 客服 人设 性格 智能体 实时数据 网站 技能 工具 转人工' },
    { path: '/shared-settings', label: '通用设置', description: '回复长度、上下文与记忆、辅助模型、技能与工具库', icon: icon(Setting), keywords: '技能 工具 记忆 上下文' },
  ] },
  { title: '知识与记忆', items: [
    { path: '/knowledge', label: '知识库', description: '按知识库管理文档、问答和待审核的知识', icon: icon(Collection), badge: 'candidates', keywords: '审核 FAQ 文档 上传 缺口' },
    { path: '/memories', label: '用户记忆', description: '按群聊、私聊和用户查看机器人记住的信息', icon: icon(Reading) },
    { path: '/evaluation', label: '效果评测', description: '用固定问题对比改动前后的回答质量', icon: icon(DataAnalysis) },
  ] },
  { title: '记录与系统', items: [
    { path: '/messages', label: '消息记录', description: '每条消息的回复和处理过程', icon: icon(ChatLineSquare) },
    { path: '/models', label: '模型', description: '对话模型和 Embedding 模型的配置与测试', icon: icon(Cpu) },
    { path: '/system', label: '系统状态', description: '服务状态、任务队列、全局参数和管理员密码', icon: icon(Monitor), keywords: '密码 队列' },
  ] },
]

export const navItems = navGroups.flatMap(group => group.items.map(item => ({ ...item, group: group.title })))
