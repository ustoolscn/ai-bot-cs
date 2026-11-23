<template>
  <div class="content-page page-fill">
    <PageHeader title="定时任务" description="每天按北京时间执行，生成内容后由目标群各自的 QQ 机器人发送。">
      <el-button type="primary" :icon="Plus" @click="edit()">新建定时任务</el-button>
    </PageHeader>
    <section class="table-panel">
      <el-table v-loading="loading" :data="tasks" height="100%" class="clickable-rows" @row-click="edit">
        <template #empty>
          <EmptyState v-if="error" mood="thinking" title="加载失败" :description="error"><el-button @click="load">重新加载</el-button></EmptyState>
          <EmptyState v-else title="还没有定时任务" description="例如每天早上 8 点在群里发天气，或者发前一天的群统计。"><el-button type="primary" :icon="Plus" @click="edit()">新建定时任务</el-button></EmptyState>
        </template>
        <el-table-column label="任务" min-width="260"><template #default="{ row }"><div class="stacked-cell"><strong>{{ row.name }}</strong><span>{{ row.instruction }}</span></div></template></el-table-column>
        <el-table-column label="每天" width="90"><template #default="{ row }"><span class="num">{{ row.sendTime }}</span></template></el-table-column>
        <el-table-column label="内容 / 格式" width="150"><template #default="{ row }"><div class="stacked-cell"><strong>{{ sources[row.source] || row.source }}</strong><span>{{ formats[row.format] || row.format }}</span></div></template></el-table-column>
        <el-table-column label="目标群" min-width="180"><template #default="{ row }"><span class="truncate">{{ row.allGroups ? '全部群' : row.groupIds.map(groupName).join('、') || '目标群已移除' }}</span></template></el-table-column>
        <el-table-column label="状态" width="100"><template #default="{ row }"><StatusBadge :status="row.enabled ? 'online' : 'offline'" :text="row.enabled ? '已启用' : '已暂停'" /></template></el-table-column>
        <el-table-column label="操作" width="220" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click.stop="openTest(row)">测试发送</el-button>
            <el-button link @click.stop="showRuns(row)">执行记录</el-button>
            <el-button link type="danger" @click.stop="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </section>

    <el-drawer v-model="editing" :title="form.id ? '编辑定时任务' : '新建定时任务'" size="min(560px, 96vw)">
      <el-form label-position="top" @submit.prevent="save">
        <div class="form-grid">
          <el-form-item label="任务名称"><el-input v-model="form.name" maxlength="120" placeholder="例如：每日天气预告" /></el-form-item>
          <el-form-item label="每天发送时间（北京时间）"><el-time-picker v-model="form.sendTime" format="HH:mm" value-format="HH:mm" placeholder="选择时间" :clearable="false" style="width: 100%" /></el-form-item>
        </div>
        <el-form-item label="发送到">
          <el-radio-group v-model="form.allGroups"><el-radio-button :value="false">指定群</el-radio-button><el-radio-button :value="true">全部群</el-radio-button></el-radio-group>
          <el-select v-if="!form.allGroups" v-model="form.groupIds" multiple filterable placeholder="选择一个或多个群" style="margin-top: 8px"><el-option v-for="g in groups" :key="g.id" :value="g.id" :label="groupName(g.id)" /></el-select>
          <small v-else class="form-tip">执行时包含所有已启用机器人所接待的已启用群。</small>
        </el-form-item>
        <el-form-item label="内容来源">
          <el-radio-group v-model="form.source"><el-radio-button value="web">联网查询</el-radio-button><el-radio-button value="group">昨日群统计</el-radio-button><el-radio-button value="instruction">按指令创作</el-radio-button></el-radio-group>
          <small class="form-tip">{{ form.source === 'group' ? '按每个群自己的数据分别生成。' : '每次生成一份内容，再分发到所有目标群。' }}</small>
        </el-form-item>
        <el-form-item label="内容指令"><el-input v-model="form.instruction" type="textarea" :rows="4" maxlength="4000" placeholder="查询当天杭州天气，发布气温、降雨概率和出行提醒。" /></el-form-item>
        <el-form-item label="发送格式">
          <el-radio-group v-model="form.format"><el-radio-button value="text">文字</el-radio-button><el-radio-button value="image">网页截图</el-radio-button><el-radio-button value="link">网页链接</el-radio-button></el-radio-group>
          <small v-if="form.format === 'link'" class="form-tip">群成员点开链接即可阅读，有效期 7 天。转发后，持有链接的人也能打开。</small>
        </el-form-item>
        <div class="setting-row" style="padding-bottom: 0">
          <div><strong>启用定时执行</strong><span>关闭后保留任务，但不再每天发送</span></div>
          <el-switch v-model="form.enabled" aria-label="启用定时执行" />
        </div>
      </el-form>
      <template #footer>
        <el-button @click="editing = false">取消</el-button>
        <el-button type="primary" :loading="saving" :disabled="!form.name.trim() || !form.sendTime || !form.instruction.trim() || (!form.allGroups && !form.groupIds.length)" @click="save">保存任务</el-button>
      </template>
    </el-drawer>

    <el-dialog v-model="testing" title="测试发送" width="min(480px, 95vw)">
      <p style="margin-bottom: 12px">立即执行“{{ active?.name }}”，结果会真实发送到所选的群。当天的定时发送照常执行。</p>
      <el-select v-model="testGroups" multiple filterable placeholder="选择接收测试消息的群"><el-option v-for="g in testOptions" :key="g.id" :value="g.id" :label="groupName(g.id)" /></el-select>
      <template #footer><el-button @click="testing = false">取消</el-button><el-button type="primary" :disabled="!testGroups.length" :loading="sending" @click="sendTest">发送到 {{ testGroups.length }} 个群</el-button></template>
    </el-dialog>

    <el-drawer v-model="history" :title="(active?.name || '') + ' 的执行记录'" size="min(720px, 96vw)">
      <el-table :data="runs" empty-text="还没有执行记录">
        <el-table-column label="开始时间" width="150"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></el-table-column>
        <el-table-column label="类型" width="70"><template #default="{ row }">{{ row.isTest ? '测试' : '定时' }}</template></el-table-column>
        <el-table-column label="目标群" min-width="160"><template #default="{ row }"><div class="stacked-cell"><strong>{{ row.groupName }}</strong><span>{{ row.botName }}</span></div></template></el-table-column>
        <el-table-column label="结果" min-width="140"><template #default="{ row }"><div class="stacked-cell"><StatusBadge :status="runTone(row.status)" :text="statuses[row.status] || row.status" /><span v-if="row.error" class="danger-text">{{ row.error }}</span></div></template></el-table-column>
      </el-table>
    </el-drawer>
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Plus } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import { request } from '@/api/client'
import { formatTime } from '@/api/agent'
import { useAdminEvents } from '@/composables/useAdminEvents'
import PageHeader from '@/components/PageHeader.vue'
import EmptyState from '@/components/EmptyState.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import type { Conversation } from '@/types'

const blank=()=>({id:'',name:'',sendTime:'08:00',instruction:'',source:'web',format:'text',allGroups:false,groupIds:[] as string[],enabled:true})
type Task=ReturnType<typeof blank>
type Delivery={createdAt:string;isTest:boolean;groupName:string;botName:string;status:string;error:string}
const tasks=ref<Task[]>([]),groups=ref<Conversation[]>([]),runs=ref<Delivery[]>([])
const active=ref<Task>(),form=ref(blank()),testGroups=ref<string[]>([])
const loading=ref(true),error=ref(''),editing=ref(false),saving=ref(false),testing=ref(false),sending=ref(false),history=ref(false)
const sources: Record<string,string>={web:'联网查询',group:'昨日群统计',instruction:'按指令创作'}
const formats: Record<string,string>={text:'文字',image:'网页截图',link:'网页链接'}
const runTone=(s:string)=>s==='sent'||s==='completed'?'success':s==='failed'||s==='expired'?'failed':s==='cancelled'?'offline':'processing'
const statuses: Record<string,string>={pending:'等待处理',processing:'执行中',completed:'生成完成',sent:'已发送',expired:'已过期',failed:'失败',cancelled:'已取消'}
const testOptions=computed(()=>groups.value.filter(g=>g.enabled && (active.value?.allGroups || active.value?.groupIds.includes(g.id))))
const groupName=(id:string)=>{const g=groups.value.find(g=>g.id===id);return g ? g.name+' · '+g.botName : id}
const explain=(e:unknown)=>e instanceof Error ? e.message : '请求失败'
async function load(){
 loading.value=true
 try{const [items,convs]=await Promise.all([request<Task[]>({url:'/schedules'}),api.conversations.list()])
 tasks.value=items;groups.value=convs.filter(g=>g.type==='group');error.value=''}
 catch(e){error.value=explain(e)}finally{loading.value=false}
}
function edit(row?:Task){form.value=row?{...row,groupIds:[...row.groupIds]}:blank();editing.value=true}
async function save(){if(saving.value)return;saving.value=true
 try{const t=form.value;await request({url:t.id?'/schedules/'+t.id:'/schedules',method:t.id?'PUT':'POST',data:t});editing.value=false;await load();ElMessage.success('已保存')}
 catch(e){ElMessage.error(explain(e))}finally{saving.value=false}}
async function remove(t:Task){try{await ElMessageBox.confirm('删除“'+t.name+'”及其执行记录？删除后不能恢复。','删除定时任务',{type:'warning',confirmButtonText:'删除任务',cancelButtonText:'取消',confirmButtonClass:'el-button--danger'})}catch{return}
 try{await request({url:'/schedules/'+t.id,method:'DELETE'});await load()}catch(e){ElMessage.error(explain(e))}}
let requestId=''
// getRandomValues also works on management pages served over a local HTTP connection.
function newRequestId() {
  if (typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  bytes[6] = (bytes[6] & 15) | 64
  bytes[8] = (bytes[8] & 63) | 128
  const hex = Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('')
  return [hex.slice(0,8), hex.slice(8,12), hex.slice(12,16), hex.slice(16,20), hex.slice(20)].join('-')
}
function openTest(t:Task){active.value=t;testGroups.value=[];requestId='';testing.value=true}
async function sendTest(){if(!active.value||sending.value)return;sending.value=true
 try{if(!requestId) requestId=newRequestId()
 await request({url:'/schedules/'+active.value.id+'/test',method:'POST',data:{groupIds:testGroups.value,requestId}});testing.value=false;ElMessage.success('测试已提交');await showRuns(active.value)}
 catch(e){ElMessage.error(explain(e))}finally{sending.value=false}}
async function loadRuns(){const id=active.value?.id;if(!history.value||!id)return
 try{const result=await request<Delivery[]>({url:'/schedules/'+id+'/runs'});if(active.value?.id===id)runs.value=result}catch(e){ElMessage.error(explain(e))}}
async function showRuns(t:Task){active.value=t;runs.value=[];history.value=true;await loadRuns()}
onMounted(load)
useAdminEvents(['scheduled_posts','conversations','bots'],load)
useAdminEvents(['schedule_runs','schedule_deliveries','agent_jobs','outbox_tasks','scheduled_posts'],loadRuns)
</script>
