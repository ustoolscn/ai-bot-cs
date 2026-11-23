<template>
  <el-drawer :model-value="true" :title="group.name + ' · 定时推送'" size="min(560px, 96vw)" @closed="$emit('close')">
    <el-alert v-if="error" type="error" :closable="false" :title="error"><el-button link @click="load">重试</el-button></el-alert>
    <div v-else v-loading="loading">
      <template v-if="!editing">
        <el-button type="primary" @click="edit()">添加定时推送</el-button>
        <p v-if="!loading && !items.length" class="muted">这个群还没有定时推送。</p>
        <div v-for="item in items" :key="item.id" class="drawer-section">
          <h3>每天 {{ item.sendTime }} <small>{{ item.enabled ? '已启用' : '已暂停' }} · {{ item.format === 'image' ? '网页截图' : '文字' }}</small></h3>
          <p>{{ item.instruction }}</p>
          <small v-if="item.status">上次执行 · {{ statuses[item.status] || item.status }} {{ item.error }}</small>
          <div><el-button link type="primary" @click="edit(item)">编辑</el-button><el-button link type="danger" @click="remove(item)">删除</el-button></div>
        </div>
      </template>
      <el-form v-else label-position="top" @submit.prevent="save">
        <el-form-item label="每日发送时间（北京时间）"><el-input v-model="form.sendTime" type="time" /></el-form-item>
        <el-form-item label="执行客服"><el-select v-model="form.serviceId" clearable :value-on-clear="''" placeholder="跟随本群接待客服"><el-option v-for="s in services" :key="s.id" :label="s.name" :value="s.id" /></el-select></el-form-item>
        <el-form-item label="内容来源"><el-radio-group v-model="form.source"><el-radio-button value="web">联网查询</el-radio-button><el-radio-button value="group">昨日群统计</el-radio-button><el-radio-button value="instruction">按指令创作</el-radio-button></el-radio-group>
          <small v-if="form.source === 'web'" class="form-tip">执行客服的对话模型需要支持联网搜索并返回检索凭据。</small></el-form-item>
        <el-form-item label="内容指令"><el-input v-model="form.instruction" type="textarea" :rows="5" maxlength="4000" placeholder="例如：查找最近 24 小时的 AI 资讯，选出 10 条，每条附一句摘要、发布日期和来源链接。" /></el-form-item>
        <el-form-item label="发送格式"><el-radio-group v-model="form.format"><el-radio-button value="text">文字</el-radio-button><el-radio-button value="image">HTML 网页截图</el-radio-button></el-radio-group></el-form-item>
        <el-form-item label="启用任务"><el-switch v-model="form.enabled" /></el-form-item>
        <el-button @click="editing=false">取消</el-button><el-button type="primary" :disabled="!form.sendTime || !form.instruction.trim()" :loading="saving" @click="save">保存任务</el-button>
      </el-form>
    </div>
  </el-drawer>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { Conversation } from '@/types'
import { request } from '@/api/client'
import { botServices, type BotService } from '@/api/support'

const props=defineProps<{ group: Conversation }>()
defineEmits<{ close: [] }>()
const blank=()=>({id:'',sendTime:'09:00',serviceId:'',instruction:'',source:'web',format:'text',enabled:true})
type Schedule=ReturnType<typeof blank> & {status?:string;error?:string}
const url='/conversations/'+props.group.id+'/schedules'
const items=ref<Schedule[]>([])
const services=ref<BotService[]>([])
const form=ref(blank())
const loading=ref(false)
const saving=ref(false)
const editing=ref(false)
const error=ref('')
const statuses:Record<string,string>={pending:'等待处理',processing:'执行中',sent:'已发送',failed:'失败',expired:'已过期',completed:'已完成'}
async function load(){
  loading.value=true
  error.value=''
  try { [items.value,services.value]=await Promise.all([request<Schedule[]>({url}),botServices(props.group.botId).list()]) }
  catch(e){error.value=e instanceof Error?e.message:'加载失败'}
  finally{loading.value=false}
}
function edit(item?:Schedule){form.value=item?{...item}:blank();editing.value=true}
async function save(){
  if(saving.value)return
  saving.value=true
  try {
    const item=form.value
    await request({url:item.id?url+'/'+item.id:url,method:item.id?'PUT':'POST',data:item})
    editing.value=false
    await load()
    ElMessage.success('定时推送已保存')
  }catch(e){ElMessage.error(e instanceof Error?e.message:'保存失败')}
  finally{saving.value=false}
}
async function remove(item:Schedule){
  try{await ElMessageBox.confirm('删除每天 '+item.sendTime+' 的这条推送任务？','删除定时推送')}catch{return}
  try{await request({url:url+'/'+item.id,method:'DELETE'});await load()}catch(e){ElMessage.error(e instanceof Error?e.message:'删除失败')}
}
onMounted(load)
</script>
<style scoped>
.drawer-section h3 small{font-weight:400;font-size:13px;color:var(--text-secondary)}
.drawer-section p{white-space:pre-wrap;overflow-wrap:anywhere}
</style>
