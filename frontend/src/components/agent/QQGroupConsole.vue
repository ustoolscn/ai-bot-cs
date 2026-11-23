<template>
  <el-tabs v-model="section" @tab-change="load(false)">
    <el-tab-pane v-for="tab in tabs" :key="tab.id" :name="tab.id" :label="tab.label" />
  </el-tabs>
  <section v-loading="loading">
    <el-button @click="load(false)">刷新{{ tabs.find(t=>t.id===section)?.label }}</el-button>
    <el-alert v-if="error" type="error" :closable="false" :title="error"/>
    <el-descriptions v-else-if="section==='profile' && info" :column="1" border>
      <el-descriptions-item v-for="[label,value] in profileRows" :key="label" :label="label">{{ value || '—' }}</el-descriptions-item>
    </el-descriptions>
    <el-checkbox v-if="section==='profile'" :model-value="extended" @change="setLimit">机器人开发者是本群群主或管理员（每条群消息最多回复 50 次）</el-checkbox>
    <template v-if="section==='applications'">
      <article v-for="row in applications" :key="row.join_request_id">
        <strong>{{row.username || row.member_openid}}</strong> {{row.bot?'机器人':''}} · {{row.apply_at}}
        <p>{{row.apply_source}} {{row.invited_by}} {{row.risk_tips}}</p>
        <p>{{row.verify_info?.verify_message}}</p><p v-for="qa in row.verify_info?.review_qa_list" :key="qa.question">{{qa.question}} · {{qa.answer}}</p>
        <small>{{row.member_openid}} {{row.union_openid}}</small>
        <div><el-button :disabled="busy" @click="review(row,'approve')">通过</el-button><el-button :disabled="busy" @click="reject=row;reason='';block=false">拒绝</el-button></div>
      </article>
      <p v-if="!applications.length && !error && !loading">没有待审批申请</p>
      <el-button v-if="next" @click="load(true)">更多申请</el-button>
    </template>
    <template v-if="section==='mutes'">
      <p v-if="rule">全员禁言模式 · {{rule.mode}}</p>
      <p v-for="r in rule?.schedule_rules" :key="r.task_id">{{r.start_at}} 至 {{r.end_at}} · {{r.enabled?'启用':'停用'}}</p>
      <p v-for="r in rule?.recurring_rules" :key="r.task_id">每周 {{r.weekdays?.join('、')}} · {{r.start_time}} 至 {{r.end_time}} · {{r.enabled?'启用':'停用'}}</p>
      <el-input v-model="member" placeholder="成员 OpenID"/><el-input-number v-model="minutes" :min="1" :max="43200" :precision="0"/> 分钟
      <el-button :disabled="!member.trim() || busy" @click="mute(member,'add')">新增禁言</el-button>
      <article v-for="row in muted" :key="row.member_openid">{{row.username || row.member_openid}} · {{row.mute_expire_at}}
        <el-button link :disabled="busy" @click="mute(row.member_openid,'update')">按所填时长更新</el-button><el-button link :disabled="busy" @click="mute(row.member_openid,'del')">解除禁言</el-button>
      </article>
    </template>
  </section>
  <el-dialog :model-value="!!reject" title="拒绝入群申请" append-to-body @close="reject=null">
    <p>{{reject?.username || reject?.member_openid}}</p><el-input v-model="reason" placeholder="拒绝理由（选填）"/><el-checkbox v-model="block">同时加入 QQ 群黑名单</el-checkbox>
    <template #footer><el-button @click="reject=null">取消</el-button><el-button type="danger" :loading="busy" @click="reject && review(reject,'decline')">确认拒绝</el-button></template>
  </el-dialog>
</template>
<script setup lang="ts">
import {computed,onMounted,ref} from 'vue'
import {ElMessage} from 'element-plus'
import {request} from '@/api/client'
const props=defineProps<{id:string}>()
const tabs=[{id:'profile',label:'群基本资料'},{id:'applications',label:'入群申请'},{id:'mutes',label:'禁言管理'}]
type Application={member_openid:string;join_request_id:string;username:string;union_openid:string;bot:boolean;apply_at:string;apply_source:string;invited_by:string;risk_tips:string;verify_info?:{verify_message:string;review_qa_list:{question:string;answer:string}[]}}
type Muted={member_openid:string;username:string;mute_expire_at:string}
type Rule={mode:string;schedule_rules?:{task_id:string;start_at:string;end_at:string;enabled:boolean}[];recurring_rules?:{task_id:string;weekdays:number[];start_time:string;end_time:string;enabled:boolean}[]}
type Info={group_name:string;group_finger_memo:string;group_class_text:string;group_tags:string[];group_member_num:number;group_openid:string;union_group_openid:string}
const section=ref('profile'),info=ref<Info>(),applications=ref<Application[]>([]),muted=ref<Muted[]>([]),rule=ref<Rule>(),next=ref(''),error=ref(''),loading=ref(false),busy=ref(false),member=ref(''),minutes=ref(10),reject=ref<Application|null>(null),reason=ref(''),block=ref(false)
const profileRows=computed(()=>{const v=info.value!;return [['群名',v.group_name],['群介绍',v.group_finger_memo],['分类',v.group_class_text],['标签',v.group_tags?.join('、')],['QQ 群人数',String(v.group_member_num)],['群 OpenID',v.group_openid],['跨应用群标识',v.union_group_openid]]})
const extended=ref(false)
async function setLimit(value:unknown){try{const r=await request<{extended:boolean}>({url:'/conversations/'+props.id+'/qq/limits',method:'POST',data:{extended:value===true}});extended.value=r.extended}catch(e){ElMessage.error(e instanceof Error?e.message:'保存失败')}}
onMounted(async()=>{try{extended.value=(await request<{extended:boolean}>({url:'/conversations/'+props.id+'/qq/limits'})).extended}catch(e){ElMessage.error(e instanceof Error?e.message:'读取额度失败')}})
let generation=0
async function load(more:boolean){
 const version=++generation,tab=section.value,url='/conversations/'+props.id+'/qq/'+tab
 loading.value=true;error.value=''
 try{
 if(tab==='profile'){const v=await request<Info>({url});if(version===generation)info.value=v}
 if(tab==='applications'){const v=await request<{list:Application[];next_cursor:string}>({url,params:{cursor:more?next.value:''}});if(version===generation){applications.value=[...(more?applications.value:[]),...(v.list||[])];next.value=v.next_cursor}}
 if(tab==='mutes'){const v=await request<{members:Muted[];global_rule:Rule}>({url});if(version===generation){muted.value=v.members||[];rule.value=v.global_rule}}
 }catch(e){if(version===generation)error.value=e instanceof Error?e.message:'读取 QQ 资料失败'}finally{if(version===generation)loading.value=false}
}
async function action(tab:string,data:object){if(busy.value)return;busy.value=true;try{await request({url:'/conversations/'+props.id+'/qq/'+tab,method:'POST',data});reject.value=null;ElMessage.success('QQ 已执行操作');await load(false)}catch(e){ElMessage.error(e instanceof Error?e.message:'操作失败')}finally{busy.value=false}}
function review(row:Application,op:string){return action('applications',{op,member:row.member_openid,id:row.join_request_id,reason:op==='decline'?reason.value:'',block:op==='decline'&&block.value})}
function mute(id:string,op:string){return action('mutes',{member:id.trim(),op,minutes:minutes.value})}
onMounted(()=>load(false))
</script>