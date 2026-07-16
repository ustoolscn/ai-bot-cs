<template>
  <div class="overview-page">
    <FilterSidebar v-model="filterOpen" />
    <div v-if="filterOpen" class="filter-backdrop" @click="filterOpen = false" />
    <section class="overview-content">
      <div class="mobile-overview-head"><h1>运行概览</h1><el-button :icon="Filter" @click="filterOpen = true">筛选</el-button></div>
      <div class="metrics-panel panel-surface">
        <div v-for="metric in overview.metrics" :key="metric.label" class="metric-item">
          <span>{{ metric.label }}</span><strong>{{ metric.value }} <small v-if="metric.sub">{{ metric.sub }}</small></strong>
          <p v-if="metric.trend">较昨日 <em :class="metric.good ? 'trend-good' : 'trend-bad'"><el-icon><component :is="metric.direction === 'up' ? Top : Bottom" /></el-icon>{{ metric.trend }}</em></p>
          <p v-else class="metric-unavailable">暂无对比数据</p>
        </div>
      </div>

      <div class="pipeline-grid">
        <section class="panel-surface pipeline-panel">
          <div class="panel-heading"><h2>管道健康度 <span>（QQ事件 / 上下文 / 知识检索 / 模型 / 投递）</span><el-tooltip content="展示最近处理消息在各环节的状态和耗时"><el-icon><InfoFilled /></el-icon></el-tooltip></h2><el-button link type="primary" @click="$router.push('/messages')">查看详情 <el-icon><ArrowRight /></el-icon></el-button></div>
          <div class="pipeline-table-wrap">
            <table class="pipeline-table">
              <thead><tr><th>时间</th><th>机器人</th><th>QQ 事件</th><th>上下文</th><th>知识检索</th><th>模型</th><th>投递</th><th>总耗时</th></tr></thead>
              <tbody><tr v-for="row in overview.pipelines" :key="row.id">
                <td>{{ row.time }}</td><td>{{ row.bot }}</td>
                <td><StageCell label="接收" status="success" :ms="row.eventMs" /></td>
                <td><StageCell label="构建" status="success" :ms="row.contextMs" /></td>
                <td><StageCell :label="row.retrieval.status === 'warning' ? '超时' : row.retrieval.status === 'failed' ? '失败' : '检索'" :status="row.retrieval.status" :ms="row.retrieval.ms" :sub="`命中 ${row.retrieval.hit}`" /></td>
                <td><StageCell :label="row.model.status === 'pending' ? '—' : '调用'" :status="row.model.status" :ms="row.model.ms" :sub="row.model.name" /></td>
                <td><StageCell :label="row.delivery.status === 'failed' ? '失败' : '投递'" :status="row.delivery.status" :ms="row.delivery.ms" :sub="row.delivery.status === 'failed' ? '已丢弃' : '成功'" /></td>
                <td :class="{ 'danger-text': row.totalMs > 5000 }"><strong>{{ formatDuration(row.totalMs) }}</strong></td>
              </tr><tr v-if="overview.pipelines.length === 0"><td colspan="8"><div class="table-empty"><el-icon><DataLine /></el-icon><strong>暂无管道明细</strong><span>后端尚未提供逐消息处理阶段数据</span></div></td></tr></tbody>
            </table>
          </div>
        </section>

        <section class="panel-surface knowledge-progress-panel">
          <div class="panel-heading"><h2>知识索引进度</h2><el-button link type="primary" @click="$router.push('/knowledge')">查看知识 <el-icon><ArrowRight /></el-icon></el-button></div>
          <div class="progress-overview"><span>总体进度</span><div><el-progress :percentage="overview.knowledge.progress" :stroke-width="9" :show-text="false" /><strong>{{ hasKnowledge ? `${overview.knowledge.progress}%` : '—' }}</strong></div></div>
          <div class="knowledge-stats"><div><span>知识库总数</span><strong>{{ overview.knowledge.total }}</strong></div><div><span class="legend-dot green" />已完成</div><strong>{{ overview.knowledge.completed }}</strong><div><span class="legend-dot blue" />索引中</div><strong>{{ overview.knowledge.indexing }}</strong><div><span class="legend-dot red" />失败</div><strong>{{ overview.knowledge.failed }}</strong><div><span class="legend-dot gray" />待处理</div><strong>{{ overview.knowledge.pending }}</strong></div>
          <div class="queue-title">索引中（{{ overview.knowledge.indexing }}）</div>
          <table class="queue-table"><thead><tr><th>知识库</th><th>进度</th><th>预计完成</th></tr></thead><tbody><tr v-for="item in overview.knowledge.queues" :key="item.name"><td>{{ item.name }}</td><td>{{ item.progress }}%</td><td>{{ item.eta }}</td></tr><tr v-if="overview.knowledge.queues.length === 0"><td colspan="3"><div class="table-empty compact"><el-icon><Document /></el-icon><strong>暂无索引队列明细</strong><span>当前接口仅提供任务数量</span></div></td></tr></tbody></table>
        </section>
      </div>

      <section id="alerts" class="panel-surface alerts-panel">
        <div class="panel-heading"><h2>近期告警</h2><el-button link type="primary" @click="showAllAlerts = !showAllAlerts">{{ showAllAlerts ? '收起告警' : '查看全部告警' }} <el-icon><ArrowRight /></el-icon></el-button></div>
        <div class="alert-table-wrap"><table class="alert-table"><thead><tr><th>级别</th><th>时间</th><th>机器人</th><th>告警内容</th><th>影响</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="alert in visibleAlerts" :key="alert.id"><td><span class="alert-level" :class="alert.level">{{ { critical: '严重', warning: '警告', info: '信息' }[alert.level] }}</span></td><td>{{ alert.time }}</td><td>{{ alert.bot }}</td><td>{{ alert.content }}</td><td>{{ alert.impact }}</td><td><StatusBadge :status="alert.recovered ? 'success' : 'warning'" :text="alert.recovered ? '已恢复' : '未恢复'" /></td><td><el-button link type="primary" @click="inspectAlert(alert.content)">查看详情</el-button></td></tr><tr v-if="visibleAlerts.length === 0"><td colspan="7"><div class="table-empty compact"><el-icon><Bell /></el-icon><strong>暂无告警明细</strong><span>当前系统概览接口未返回告警记录</span></div></td></tr></tbody></table></div>
      </section>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, ref } from 'vue'
import { ArrowRight, Bell, Bottom, CircleCheck, CircleClose, DataLine, Document, Filter, InfoFilled, Remove, Top, Warning } from '@element-plus/icons-vue'
import { ElMessageBox } from 'element-plus'
import { api } from '@/api'
import FilterSidebar from '@/components/FilterSidebar.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import type { SystemOverview } from '@/types'
import { formatDuration } from '@/utils/format'
import { createEmptyOverview } from '@/utils/overview'

const overview = ref<SystemOverview>(createEmptyOverview())
const filterOpen = ref(false)
const showAllAlerts = ref(false)
const visibleAlerts = computed(() => showAllAlerts.value ? overview.value.alerts : overview.value.alerts.slice(0, 4))
const hasKnowledge = computed(() => overview.value.knowledge.total > 0)
onMounted(async () => { overview.value = await api.overview() })
function inspectAlert(content: string) { ElMessageBox.alert(content, '告警详情', { confirmButtonText: '知道了' }) }

const StageCell = defineComponent({
  props: { label: String, status: { type: String, required: true }, ms: Number, sub: String },
  setup(props) {
    return () => h('div', { class: ['stage-cell', `is-${props.status}`] }, [
      h('div', { class: 'stage-main' }, [h('span', props.label), h(props.status === 'success' ? CircleCheck : props.status === 'warning' ? Warning : props.status === 'failed' ? CircleClose : Remove, { class: 'stage-icon' }), props.ms !== undefined ? h('small', formatDuration(props.ms)) : null]),
      props.sub ? h('small', { class: 'stage-sub' }, props.sub) : null,
    ])
  },
})
</script>
