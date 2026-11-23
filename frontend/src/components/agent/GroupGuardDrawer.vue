<template>
  <el-drawer :model-value="true" :title="group.name+' · 群管理'" size="min(600px, 96vw)" @closed="$emit('close')">
    <el-alert v-if="error" type="error" :title="error" :closable="false"><el-button link @click="load">重试</el-button></el-alert>
    <el-form v-else v-loading="loading" label-position="top">
      <h3>入群自动审批</h3>
      <el-form-item label="自动通过的验证答案"><el-input v-model="form.joinAnswer" maxlength="200" placeholder="留空关闭，例如：活动报名口令" /><small class="form-tip">新申请的验证消息或问答答案与此项完全一致时通过。其他申请留给管理员处理。需订阅 QQ 群成员事件。</small></el-form-item>
      <h3>发言审核</h3>
      <el-form-item label="审核收到的群消息"><el-switch v-model="form.enabled" /></el-form-item>
      <template v-if="form.enabled">
        <el-form-item label="违规关键词"><el-input v-model="form.keywords" type="textarea" :rows="3" placeholder="每行一个，发言包含任一关键词即命中" /></el-form-item>
        <el-form-item label="由 AI 判断的违规意图"><el-input v-model="form.intent" type="textarea" :rows="3" placeholder="例如：推广其他中转站、引导群友私聊购买服务。正常咨询或举报广告可以放行。" /><small class="form-tip">没有命中关键词时，判断模型会按本群意图审核文字和图片，包括只发图片的消息。请为接待客服选择支持看图的判断模型。</small></el-form-item>
        <el-form-item label="命中后的处理"><el-checkbox v-model="form.retract">撤回消息</el-checkbox><el-checkbox v-model="form.block">加入本群客服黑名单</el-checkbox></el-form-item>
        <el-form-item label="禁言时长（分钟，0 表示关闭）"><el-input-number v-model="form.muteMinutes" :min="0" :max="43200" :precision="0" /></el-form-item>
      </template>
      <p class="muted">QQ 要求机器人具有群管理员身份才能审批、禁言和撤回普通成员发言。撤回时限为 2 分钟。</p>
      <el-button type="primary" :disabled="loading" :loading="saving" @click="save">保存群规则</el-button>
      <div class="drawer-section"><h3>最近处理记录 <el-button link @click="loadActions">刷新</el-button></h3>
        <p v-if="!actions.length" class="muted">暂无记录</p>
        <div v-for="(a,i) in actions" :key="i" class="content-box gap"><strong>{{ labels[a.kind] }} · {{ labels[a.status] || a.status }}</strong><br />{{ a.reason }}<br /><small>{{ a.userId }} · {{ formatTime(a.createdAt) }}</small><p v-if="a.detail">{{ a.detail }}</p></div>
      </div>
    </el-form>
  </el-drawer>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { request } from '@/api/client'
import { formatTime } from '@/api/agent'
import type { Conversation } from '@/types'
const props=defineProps<{group:Conversation}>()
defineEmits<{close:[]}>()
const url='/conversations/'+props.group.id+'/guard'
const form=ref({enabled:false,keywords:'',intent:'',muteMinutes:0,retract:false,block:false,joinAnswer:''})
const actions=ref<Array<{kind:string;status:string;userId:string;reason:string;detail:string;createdAt:string}>>([])
const loading=ref(true),saving=ref(false),error=ref('')
const labels:Record<string,string>={approve:'通过入群申请',retract:'撤回消息',mute:'禁言',block:'加入客服黑名单',done:'成功',failed:'失败',expired:'已过期',pending:'等待处理',processing:'处理中',skipped:'已跳过',unknown:'状态待核实',review:'AI 审核'}
async function loadActions(){try{actions.value=await request({url:url+'/actions'})}catch(e){ElMessage.error(e instanceof Error?e.message:'记录加载失败')}}
async function load(){loading.value=true;error.value='';try{form.value=await request({url});await loadActions()}catch(e){error.value=e instanceof Error?e.message:'加载失败'}finally{loading.value=false}}
async function save(){saving.value=true;try{form.value=await request({url,method:'PUT',data:form.value});ElMessage.success('群规则已保存')}catch(e){ElMessage.error(e instanceof Error?e.message:'保存失败')}finally{saving.value=false}}
onMounted(load)
useAdminEvents(['group_guard_actions','group_reviews','agent_jobs'], loadActions)
</script>
