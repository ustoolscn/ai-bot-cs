<template>
  <section v-loading="loading" class="panel">
    <div class="panel-head">
      <div><h2>群规则</h2><p>机器人按这些规则自动处理入群申请和群消息。QQ 要求机器人是群管理员才能审批、禁言和撤回普通成员的发言，撤回时限为 2 分钟。</p></div>
    </div>
    <div class="subsection" style="border-top: 0">
      <div class="subsection-head"><div><h3>入群自动审批</h3><p>新申请的验证消息或问答答案和下面的内容完全一致时自动通过，其他申请留给管理员处理。需要订阅 QQ 群成员事件。</p></div></div>
      <el-input v-model="form.joinAnswer" maxlength="200" placeholder="留空表示关闭，例如：活动报名口令" style="max-width: 420px" />
    </div>
    <div class="subsection">
      <div class="setting-row" style="padding: 0">
        <div><strong>发言审核</strong><span>审核收到的群消息，命中后按下面的方式处理</span></div>
        <el-switch v-model="form.enabled" aria-label="发言审核" />
      </div>
      <el-form v-if="form.enabled" label-position="top" style="margin-top: 16px">
        <div class="form-grid">
          <el-form-item label="违规关键词（每行一个）"><el-input v-model="form.keywords" type="textarea" :rows="4" placeholder="发言包含任一关键词即命中" /></el-form-item>
          <el-form-item label="由 AI 判断的违规意图"><el-input v-model="form.intent" type="textarea" :rows="4" placeholder="例如：推广其他中转站、引导群友私聊购买服务。正常咨询或举报广告可以放行。" /></el-form-item>
        </div>
        <p class="form-tip" style="margin: -8px 0 16px">没有命中关键词时，判断模型会按意图审核文字和图片，包括只发图片的消息。请为接待客服选择支持看图的判断模型。</p>
        <el-form-item label="命中后的处理" style="margin-bottom: 0">
          <div class="fact-row" style="align-items: center">
            <el-checkbox v-model="form.retract">撤回消息</el-checkbox>
            <el-checkbox v-model="form.block">加入本群黑名单</el-checkbox>
            <span>禁言 <el-input-number v-model="form.muteMinutes" :min="0" :max="43200" :precision="0" size="small" style="width: 120px; margin: 0 6px" /> 分钟（0 表示不禁言）</span>
          </div>
        </el-form-item>
      </el-form>
    </div>
    <div class="panel-foot">
      <span class="note">{{ dirty ? '有未保存的修改' : '规则已是最新' }}</span>
      <el-button :disabled="!dirty" @click="reset">放弃修改</el-button>
      <el-button type="primary" :disabled="loading || !dirty" :loading="saving" @click="save">保存群规则</el-button>
    </div>
  </section>

  <section class="panel">
    <div class="panel-head">
      <div><h2>处理记录</h2><p>最近按群规则执行的审批、撤回、禁言和拉黑。</p></div>
      <el-button :icon="Refresh" @click="loadActions">刷新</el-button>
    </div>
    <el-table :data="actions" max-height="480" empty-text="还没有处理记录">
      <el-table-column label="操作" width="150"><template #default="{ row }">{{ labels[row.kind] || row.kind }}</template></el-table-column>
      <el-table-column label="结果" width="110"><template #default="{ row }"><span class="pill" :class="toneOf(row.status)">{{ labels[row.status] || row.status }}</span></template></el-table-column>
      <el-table-column label="原因" min-width="220"><template #default="{ row }"><div class="stacked-cell"><strong>{{ $at(row.reason) || '—' }}</strong><span v-if="row.detail">{{ $at(row.detail) }}</span></div></template></el-table-column>
      <el-table-column label="成员" width="180"><template #default="{ row }"><span class="mono">{{ row.userId }}</span></template></el-table-column>
      <el-table-column label="时间" width="150"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></el-table-column>
    </el-table>
  </section>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, onMounted, ref } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { request } from '@/api/client'
import { formatTime } from '@/api/agent'

const props = defineProps<{ conversationId: string }>()
const url = '/conversations/' + props.conversationId + '/guard'
type Rules = { enabled: boolean; keywords: string; intent: string; muteMinutes: number; retract: boolean; block: boolean; joinAnswer: string }
const form = ref<Rules>({ enabled: false, keywords: '', intent: '', muteMinutes: 0, retract: false, block: false, joinAnswer: '' })
const snapshot = ref(JSON.stringify(form.value))
const dirty = computed(() => JSON.stringify(form.value) !== snapshot.value)
const actions = ref<Array<{ kind: string; status: string; userId: string; reason: string; detail: string; createdAt: string }>>([])
const loading = ref(true)
const saving = ref(false)
const labels: Record<string, string> = { approve: '通过入群申请', retract: '撤回消息', mute: '禁言', block: '加入黑名单', review: 'AI 审核', done: '成功', failed: '失败', expired: '已过期', pending: '等待处理', processing: '处理中', skipped: '已跳过', unknown: '待核实' }
function toneOf(status: string) { return status === 'done' ? 'good' : status === 'failed' ? 'bad' : status === 'pending' || status === 'processing' ? 'busy' : 'idle' }
function apply(value: Rules) { form.value = value; snapshot.value = JSON.stringify(value) }
function reset() { form.value = JSON.parse(snapshot.value) }

async function loadActions() {
  try { actions.value = await request({ url: url + '/actions' }) } catch (e) { ElMessage.error(e instanceof Error ? e.message : '处理记录加载失败') }
}
async function load() {
  loading.value = true
  try { apply(await request<Rules>({ url })); await loadActions() }
  catch (e) { ElMessage.error(e instanceof Error ? e.message : '群规则加载失败') }
  finally { loading.value = false }
}
async function save() {
  saving.value = true
  try { apply(await request<Rules>({ url, method: 'PUT', data: form.value })); ElMessage.success('群规则已保存') }
  catch (e) { ElMessage.error(e instanceof Error ? e.message : '保存失败') }
  finally { saving.value = false }
}
onMounted(load)
useAdminEvents(['group_guard_actions', 'group_reviews', 'agent_jobs'], loadActions)
</script>
