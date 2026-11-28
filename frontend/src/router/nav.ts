import { markRaw, type Component } from 'vue'
import {
  Calendar,
  ChatDotRound,
  ChatLineSquare,
  Collection,
  Connection,
  Cpu,
  Headset,
  Monitor,
  Odometer,
  Reading,
  Setting,
} from '@element-plus/icons-vue'

export interface NavItem {
  path: string
  label: string
  description: string
  icon: Component
  badge?: 'handoffs' | 'candidates'
  keywords?: string
}
export interface NavGroup {
  title: string
  items: NavItem[]
}

const icon = (value: Component) => markRaw(value)

// 4 大清晰板块 × 11 项高内聚菜单：指挥中心 -> 社群互动 -> AI 与认知 -> 引擎与运维
export const navGroups: NavGroup[] = [
  {
    title: '指挥中心',
    items: [
      {
        path: '/overview',
        label: '运行总览',
        description: 'AI 神经中枢实时动态、待办、消费与队列健康',
        icon: icon(Odometer),
        keywords: '首页 概览 告警 成功率 费用',
      },
      {
        path: '/handoffs',
        label: '人工接管台',
        description: '转人工的工单用户，在这里直接接单与回复',
        icon: icon(Headset),
        badge: 'handoffs',
        keywords: '转人工 回复 结单 工单',
      },
    ],
  },
  {
    title: '社群互动',
    items: [
      {
        path: '/conversations',
        label: '群与会话',
        description: '像 QQ 一样直接在群里聊天，接待设置与群管理在设置里',
        icon: icon(ChatDotRound),
        keywords: '成员 入群 审批 禁言 发言审核 发送 群管理 客服学习',
      },
      {
        path: '/operations',
        label: '群运营与风控',
        description: '整合每日定时播报任务与群聊黑名单风控管理',
        icon: icon(Calendar),
        keywords: '定时 发送 天气 日报 黑名单 拉黑 踢人 风控',
      },
    ],
  },
  {
    title: 'AI 与认知',
    items: [
      {
        path: '/knowledge',
        label: '知识宇宙',
        description: '3D 知识全景大屏：管理文档、问答、待审核知识与知识缺口',
        icon: icon(Collection),
        badge: 'candidates',
        keywords: '知识库 审核 FAQ 文档 上传 缺口 3D',
      },
      {
        path: '/memories',
        label: '记忆星图',
        description: '3D 跨机器人人物星图、关系演进、群情景记忆与主动关怀',
        icon: icon(Reading),
        keywords: '用户记忆 画像 关系 群情景 主动关怀 UnionID 3D',
      },
      {
        path: '/bots',
        label: '机器人与客服',
        description: 'QQ 机器人接入及旗下 AI 客服的人设、陪伴、网站实时数据与工具',
        icon: icon(Connection),
        keywords: 'AppID 回调 AI 客服 人设 性格 智能体 实时数据 网站 技能 工具 转人工',
      },
      {
        path: '/studio',
        label: '智能体工坊',
        description: '整合通用回复与记忆策略、技能工具库及效果评测实验室',
        icon: icon(Setting),
        keywords: '通用设置 技能 工具 记忆 上下文 辅助模型 效果评测 评测',
      },
    ],
  },
  {
    title: '引擎与运维',
    items: [
      {
        path: '/models',
        label: '模型与代理',
        description: '对话/判断/向量/生图模型配置测试，以及网络代理中继管理',
        icon: icon(Cpu),
        keywords: '模型 代理 proxy 测试 价格 延迟 DeepSeek Gemini',
      },
      {
        path: '/messages',
        label: '消息追踪',
        description: '每条消息的完整执行链路追踪与日志导出',
        icon: icon(ChatLineSquare),
        keywords: '消息记录 日志 失败 耗时 导出',
      },
      {
        path: '/system',
        label: '运维与系统',
        description: '实时服务拓扑、任务队列处置、公网地址、全局参数与安全',
        icon: icon(Monitor),
        keywords: '系统状态 系统设置 服务 队列 失败任务 公网地址 全局参数 密码',
      },
    ],
  },
]

export const navItems = navGroups.flatMap(group => group.items.map(item => ({ ...item, group: group.title })))
