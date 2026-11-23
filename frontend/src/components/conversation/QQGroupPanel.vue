<template>
  <section v-loading="loading.profile" class="panel">
    <div class="panel-head">
      <div><h2>QQ 群资料</h2><p>从 QQ 实时读取。</p></div>
      <el-button :icon="Refresh" @click="loadProfile">刷新</el-button>
    </div>
    <div class="panel-body">
      <dl v-if="info" class="detail-list">
        <div v-for="[label, value] in profileRows" :key="label"><dt>{{ label }}</dt><dd :class="{ mono: label.includes('标识') || label.includes('OpenID') }">{{ value || '—' }}</dd></div>
      </dl>
      <p v-else-if="!loading.profile" class="muted">没有读到群资料。</p>
      <div class="setting-row" style="margin-top: 16px; padding-bottom: 0; border-top: 0">
        <div><strong>机器人开发者是本群群主或管理员</strong><span>打开后，每条群消息最多可以回复 50 次</span></div>
        <el-switch :model-value="extended" aria-label="扩大回复额度" @change="setLimit" />
      </div>
    </div>
  </section>

  <section v-loading="loading.applications" class="panel">
    <div class="panel-head">
      <div><h2>入群申请<span v-if="applications.length" class="count-pill">{{ applications.length }}</span></h2><p>没被自动审批通过的申请，在这里手动处理。</p></div>
      <el-button :icon="Refresh" @click="loadApplications(false)">刷新</el-button>
    </div>
    <div class="panel-body">
      <div v-if="applications.length" class="record-list">
        <div v-for="row in applications" :key="row.join_request_id" class="record-item">
          <div class="stacked-cell">
            <strong>{{ row.username || row.member_openid }}<span v-if="row.bot" class="muted"> · 机器人</span></strong>
            <span>{{ [row.apply_at, row.apply_source, row.invited_by && '邀请人 ' + row.invited_by].filter(Boolean).join(' · ') }}</span>
          </div>
          <div class="record-actions">
            <el-button size="small" type="primary" plain :disabled="busy" @click="review(row, 'approve')">通过</el-button>
            <el-button size="small" :disabled="busy" @click="openReject(row)">拒绝</el-button>
          </div>
          <p v-if="row.verify_info?.verify_message || row.verify_info?.review_qa_list?.length">
            <template v-if="row.verify_info?.verify_message">验证消息：{{ row.verify_info.verify_message }}</template>
            <template v-for="qa in row.verify_info?.review_qa_list" :key="qa.question"><br />{{ qa.question }}：{{ qa.answer }}</template>
          </p>
          <p v-if="row.risk_tips" class="danger-text">风险提示：{{ row.risk_tips }}</p>
        </div>
      </div>
      <p v-else-if="!loading.applications" class="muted">没有待审批的入群申请。</p>
      <el-button v-if="nextCursor" style="margin-top: 12px" @click="loadApplications(true)">加载更多申请</el-button>
    </div>
  </section>

  <section v-loading="loading.mutes" class="panel">
    <div class="panel-head">
      <div><h2>禁言</h2><p>{{ ruleSummary }}</p></div>
      <el-button :icon="Refresh" @click="loadMutes">刷新</el-button>
    </div>
    <div class="panel-body">
      <div class="form-row" style="align-items: center; flex-wrap: wrap">
        <el-input v-model="member" placeholder="成员 OpenID" style="width: 280px" />
        <span class="fact-row" style="align-items: center">禁言 <el-input-number v-model="minutes" :min="1" :max="43200" :precision="0" style="width: 120px" /> 分钟</span>
        <el-button :disabled="!member.trim() || busy" @click="mute(member, 'add')">禁言该成员</el-button>
      </div>
      <div v-if="rule?.schedule_rules?.length || rule?.recurring_rules?.length" class="record-list" style="margin-top: 16px">
        <div v-for="r in rule?.schedule_rules" :key="r.task_id" class="record-item"><span>全员禁言 {{ r.start_at }} 至 {{ r.end_at }}</span><span class="pill" :class="r.enabled ? 'good' : 'idle'">{{ r.enabled ? '启用' : '停用' }}</span></div>
        <div v-for="r in rule?.recurring_rules" :key="r.task_id" class="record-item"><span>每周 {{ r.weekdays?.join('、') }} · {{ r.start_time }} 至 {{ r.end_time }}</span><span class="pill" :class="r.enabled ? 'good' : 'idle'">{{ r.enabled ? '启用' : '停用' }}</span></div>
      </div>
    </div>
    <el-table :data="muted" max-height="360" empty-text="当前没有被禁言的成员">
      <el-table-column label="成员" min-width="200"><template #default="{ row }"><div class="stacked-cell"><strong>{{ row.username || '未知昵称' }}</strong><span class="mono">{{ row.member_openid }}</span></div></template></el-table-column>
      <el-table-column label="禁言到" width="180" prop="mute_expire_at" />
      <el-table-column label="操作" width="220" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" :disabled="busy" @click="mute(row.member_openid, 'update')">改为 {{ minutes }} 分钟</el-button>
          <el-button link :disabled="busy" @click="mute(row.member_openid, 'del')">解除禁言</el-button>
        </template>
      </el-table-column>
    </el-table>
  </section>

  <el-dialog :model-value="!!rejecting" title="拒绝入群申请" width="440px" append-to-body @close="rejecting = null">
    <p style="margin-bottom: 12px">拒绝 <strong>{{ rejecting?.username || rejecting?.member_openid }}</strong> 的入群申请。</p>
    <el-input v-model="reason" placeholder="拒绝理由（选填）" />
    <el-checkbox v-model="block" style="margin-top: 12px">同时加入 QQ 群黑名单</el-checkbox>
    <template #footer><el-button @click="rejecting = null">取消</el-button><el-button type="danger" :loading="busy" @click="rejecting && review(rejecting, 'decline')">拒绝申请</el-button></template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { request } from '@/api/client'

const props = defineProps<{ conversationId: string }>()
const base = '/conversations/' + props.conversationId + '/qq/'
type Application = { member_openid: string; join_request_id: string; username: string; union_openid: string; bot: boolean; apply_at: string; apply_source: string; invited_by: string; risk_tips: string; verify_info?: { verify_message: string; review_qa_list: { question: string; answer: string }[] } }
type Muted = { member_openid: string; username: string; mute_expire_at: string }
type Rule = { mode: string; schedule_rules?: { task_id: string; start_at: string; end_at: string; enabled: boolean }[]; recurring_rules?: { task_id: string; weekdays: number[]; start_time: string; end_time: string; enabled: boolean }[] }
type Info = { group_name: string; group_finger_memo: string; group_class_text: string; group_tags: string[]; group_member_num: number; group_openid: string; union_group_openid: string }

const loading = reactive({ profile: false, applications: false, mutes: false })
const info = ref<Info>()
const extended = ref(false)
const applications = ref<Application[]>([])
const nextCursor = ref('')
const muted = ref<Muted[]>([])
const rule = ref<Rule>()
const busy = ref(false)
const member = ref('')
const minutes = ref(10)
const rejecting = ref<Application | null>(null)
const reason = ref('')
const block = ref(false)

const profileRows = computed(() => {
  const v = info.value!
  return [['群名', v.group_name], ['群人数', v.group_member_num != null ? String(v.group_member_num) : ''], ['分类', v.group_class_text], ['标签', v.group_tags?.join('、')], ['群介绍', v.group_finger_memo], ['群 OpenID', v.group_openid], ['跨应用群标识', v.union_group_openid]]
})
const ruleSummary = computed(() => rule.value?.mode ? `全员禁言模式：${rule.value.mode}。也可以按成员单独禁言。` : '按成员单独禁言或解除禁言。')
const explain = (e: unknown, fallback: string) => e instanceof Error ? e.message : fallback

async function loadProfile() {
  loading.profile = true
  try {
    const [profile, limits] = await Promise.all([request<Info>({ url: base + 'profile' }), request<{ extended: boolean }>({ url: base + 'limits' })])
    info.value = profile
    extended.value = limits.extended
  } catch (e) { ElMessage.error(explain(e, '读取 QQ 群资料失败')) } finally { loading.profile = false }
}
async function loadApplications(more: boolean) {
  loading.applications = true
  try {
    const v = await request<{ list: Application[]; next_cursor: string }>({ url: base + 'applications', params: { cursor: more ? nextCursor.value : '' } })
    applications.value = [...(more ? applications.value : []), ...(v.list || [])]
    nextCursor.value = v.next_cursor
  } catch (e) { ElMessage.error(explain(e, '读取入群申请失败')) } finally { loading.applications = false }
}
async function loadMutes() {
  loading.mutes = true
  try {
    const v = await request<{ members: Muted[]; global_rule: Rule }>({ url: base + 'mutes' })
    muted.value = v.members || []
    rule.value = v.global_rule
  } catch (e) { ElMessage.error(explain(e, '读取禁言列表失败')) } finally { loading.mutes = false }
}
async function setLimit(value: unknown) {
  try { extended.value = (await request<{ extended: boolean }>({ url: base + 'limits', method: 'POST', data: { extended: value === true } })).extended }
  catch (e) { ElMessage.error(explain(e, '保存失败')) }
}
async function action(tab: 'applications' | 'mutes', data: object) {
  if (busy.value) return
  busy.value = true
  try {
    await request({ url: base + tab, method: 'POST', data })
    rejecting.value = null
    ElMessage.success('QQ 已执行操作')
    if (tab === 'applications') await loadApplications(false); else await loadMutes()
  } catch (e) { ElMessage.error(explain(e, '操作失败')) } finally { busy.value = false }
}
function openReject(row: Application) { rejecting.value = row; reason.value = ''; block.value = false }
function review(row: Application, op: 'approve' | 'decline') {
  return action('applications', { op, member: row.member_openid, id: row.join_request_id, reason: op === 'decline' ? reason.value : '', block: op === 'decline' && block.value })
}
function mute(id: string, op: 'add' | 'update' | 'del') { return action('mutes', { member: id.trim(), op, minutes: minutes.value }) }

onMounted(() => { void loadProfile(); void loadApplications(false); void loadMutes() })
</script>
