import { markRaw, type Component } from 'vue'
import { ChatDotRound, ChatLineSquare, Collection, Connection, Cpu, DataAnalysis, Headset, MagicStick, CircleClose, Odometer, Reading, Setting } from '@element-plus/icons-vue'

export interface NavItem { path: string; label: string; description: string; icon: Component; badge?: 'handoffs' | 'candidates' }
export interface NavGroup { title: string; items: NavItem[] }

const icon = (value: Component) => markRaw(value)

export const navGroups: NavGroup[] = [
  { title: '工作台', items: [
    { path: '/overview', label: '今日概览', description: '待办事项、核心指标和服务健康', icon: icon(Odometer) },
    { path: '/handoffs', label: '人工台', description: '转人工的用户，在这里直接回复', icon: icon(Headset), badge: 'handoffs' },
    { path: '/learning', label: '知识审核', description: '候选知识、FAQ、知识缺口、反馈和用户记忆', icon: icon(Reading), badge: 'candidates' },
  ] },
  { title: '运行记录', items: [
    { path: '/messages', label: '消息记录', description: '每条消息的回复和处理链路', icon: icon(ChatLineSquare) },
    { path: '/conversations', label: '群与会话', description: '每个群或私聊的触发方式、模型和知识库', icon: icon(ChatDotRound) },
    { path: '/blacklist', label: '黑名单', description: '拉黑的群成员，机器人不理睬并自动踢出群', icon: icon(CircleClose) },
  ] },
  { title: '客服配置', items: [
    { path: '/agent', label: '智能体', description: '人设、回复长度、记忆、网站数据、技能与工具', icon: icon(MagicStick) },
    { path: '/knowledge', label: '知识库', description: '上传文档、索引进度和检索测试', icon: icon(Collection) },
    { path: '/evaluation', label: '效果评测', description: '用固定问题对比改动前后的回答质量', icon: icon(DataAnalysis) },
  ] },
  { title: '接入设置', items: [
    { path: '/bots', label: '机器人', description: 'QQ 机器人凭证、回调地址和默认模型', icon: icon(Connection) },
    { path: '/models', label: '模型', description: '对话模型和 Embedding 模型配置与测试', icon: icon(Cpu) },
    { path: '/system', label: '系统状态', description: '服务状态、任务队列、全局参数和管理员密码', icon: icon(Setting) },
  ] },
]

export const navItems = navGroups.flatMap(group => group.items.map(item => ({ ...item, group: group.title })))
