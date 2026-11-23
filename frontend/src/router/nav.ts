import { markRaw, type Component } from 'vue'
import { ChatDotRound, ChatLineSquare, Collection, Connection, Cpu, DataAnalysis, Headset, MagicStick, CircleClose, Clock, Odometer, Reading, Setting } from '@element-plus/icons-vue'

export interface NavItem { path: string; label: string; description: string; icon: Component; badge?: 'handoffs' | 'candidates' }
export interface NavGroup { title: string; items: NavItem[] }

const icon = (value: Component) => markRaw(value)

export const navGroups: NavGroup[] = [
  { title: '工作台', items: [
    { path: '/overview', label: '今日概览', description: '待办事项、核心指标和服务健康', icon: icon(Odometer) },
    { path: '/schedules', label: '定时任务', description: '设置发送时间、内容和目标群，测试发送并查看结果', icon: icon(Clock) },
    { path: '/handoffs', label: '人工台', description: '转人工的用户，在这里直接回复', icon: icon(Headset), badge: 'handoffs' },
    { path: '/memories', label: '记忆', description: '按群聊、私聊和用户管理记忆', icon: icon(Reading) },
  ] },
  { title: '运行记录', items: [
    { path: '/messages', label: '消息记录', description: '每条消息的回复和处理链路', icon: icon(ChatLineSquare) },
    { path: '/conversations', label: '群与会话', description: '每个群或私聊的接待客服、触发方式和知识库', icon: icon(ChatDotRound) },
    { path: '/blacklist', label: '黑名单', description: '拉黑的群成员，机器人不理睬并自动踢出群', icon: icon(CircleClose) },
  ] },
  { title: '客服配置', items: [
    { path: '/bots', label: 'QQ 机器人', description: '机器人接入与 AI 客服', icon: icon(Connection) },
    { path: '/agent', label: '智能体', description: '客服职责和选用的性格', icon: icon(MagicStick) },
    { path: '/personalities', label: '性格', description: '可复用的说话风格', icon: icon(ChatDotRound) },
    { path: '/shared-settings', label: '通用设置', description: '默认客服、回复长度、记忆与工具', icon: icon(Setting) },
    { path: '/knowledge', label: '知识库', description: '文档、知识问答、待审核、知识缺口和反馈', icon: icon(Collection), badge: 'candidates' },
    { path: '/evaluation', label: '效果评测', description: '用固定问题对比改动前后的回答质量', icon: icon(DataAnalysis) },
  ] },
  { title: '接入设置', items: [
    { path: '/models', label: '模型', description: '对话模型和 Embedding 模型配置与测试', icon: icon(Cpu) },
    { path: '/system', label: '系统状态', description: '服务状态、任务队列、全局参数和管理员密码', icon: icon(Setting) },
  ] },
]

export const navItems = navGroups.flatMap(group => group.items.map(item => ({ ...item, group: group.title })))
