<template>
<el-drawer :model-value="true" :title="'发送到 '+row.name" size="min(620px,96vw)" @closed="$emit('close')">
<el-tabs v-model="tab"><el-tab-pane label="发消息" name="send"/><el-tab-pane label="上传素材" name="upload"/></el-tabs>
<el-form v-if="tab==='send'" label-position="top">
  <el-form-item label="消息类型"><el-select v-model="form.kind"><el-option v-for="v in kinds" :key="v[0]" :value="v[0]" :label="v[1]"/></el-select></el-form-item>
  <el-input v-if="['text','markdown'].includes(form.kind)" v-model="form.text" type="textarea" placeholder="消息正文"/>
  <template v-if="form.kind==='card'"><el-input v-model="form.title" placeholder="卡片标题" maxlength="50"/><el-input v-model="form.description" placeholder="卡片描述" maxlength="200"/><el-input v-model="form.url" placeholder="HTTPS 跳转地址"/><el-input v-model="form.picture" placeholder="HTTPS 图片地址"/></template>
  <el-form-item v-if="form.kind==='typing'" label="显示输入状态的秒数"><el-input-number v-model="form.seconds" :min="1" :max="60"/></el-form-item>
  <el-form-item v-if="form.kind==='media'" label="已上传素材"><el-select v-model="form.asset"><el-option v-for="a in assets" :key="a.id" :label="a.filename || a.file_uuid" :value="a.id"/></el-select></el-form-item>
  <el-form-item label="发送方式"><el-select v-model="mode"><el-option label="主动发送" value="active"/><el-option label="回复收到的消息" value="reply"/><el-option label="回复事件" value="event"/><el-option v-if="privateChat" label="互动召回" value="wakeup"/></el-select></el-form-item>
  <el-select v-if="mode==='event'" v-model="form.event" placeholder="选择 QQ 事件"><el-option v-for="s in options.filter(s=>s.kind==='event')" :key="s.id" :value="s.id" :label="s.label+' · '+s.at"/></el-select>
  <template v-if="mode==='reply'"><el-select v-model="form.reply" placeholder="选择要回复的消息"><el-option v-for="s in options.filter(s=>s.passive)" :key="s.id" :value="s.id" :label="s.label+' · '+s.at"/></el-select><el-checkbox v-if="!privateChat" v-model="privateReply">私聊回复该群消息的发送者（需为好友）</el-checkbox></template>
  <el-form-item v-if="!sendPrivateReply" label="原生引用"><el-select v-model="form.reference" clearable placeholder="可引用群友或机器人发过的消息"><el-option v-for="s in options.filter(s=>s.quotable)" :key="s.id" :value="s.id" :label="(s.direction==='outbound'?'机器人 · ':'')+s.label+' · '+s.at"/></el-select></el-form-item>
  <el-form-item label="内嵌键盘"><el-select v-model="keyboardMode"><el-option label="无" value="none"/><el-option label="QQ 模板 ID" value="template"/><el-option label="自定义按钮布局" value="custom"/></el-select></el-form-item>
  <el-input v-if="keyboardMode==='template'" v-model="form.keyboard" placeholder="键盘模板 ID"/><el-input v-if="keyboardMode==='custom'" v-model="layout" type="textarea" placeholder='{"rows":[{"buttons":[…]}]}'/>
  <el-button type="primary" :loading="busy" @click="send">发送</el-button>
</el-form>
<el-form v-else label-position="top"><el-form-item label="素材类型"><el-select v-model="fileKind"><el-option v-for="(label,n) in ['图片','视频','语音','文件']" :key="n" :label="label" :value="n+1"/></el-select></el-form-item>
<input type="file" @change="choose"/><el-input v-if="!file" v-model="remote" placeholder="或填写 HTTPS 文件地址"/>
<el-checkbox v-model="direct">上传后直接发送到当前会话</el-checkbox>
<el-button :loading="busy" @click="upload">{{direct?'上传并发送':'上传素材'}}</el-button>
<article v-for="a in assets" :key="a.id"><strong>{{a.filename || a.file_uuid}}</strong> · {{a.expires_at || '长期有效'}} <a v-if="a.raw_url?.startsWith('https://')" :href="a.raw_url" target="_blank" rel="noreferrer">下载</a></article>
</el-form>
</el-drawer>
</template>
<script setup lang="ts">
import {ref,reactive,computed,onMounted} from 'vue'
import {ElMessage} from 'element-plus'
import {request} from '@/api/client'
import type {Conversation} from '@/types'
const props=defineProps<{row:Conversation}>();defineEmits<{close:[]}>()
const base='/conversations/'+props.row.id+'/delivery/',privateChat=props.row.type==='c2c'
const kinds=[['text','文本'],['markdown','Markdown'],['card','图文卡片'],['media','素材'],...(privateChat?[['typing','输入中状态']]:[])]
const options=ref<{id:string;kind:string;direction:string;passive:boolean;quotable:boolean;label:string;at:string}[]>([])
const assets=ref<{id:string;filename:string;file_uuid:string;expires_at:string;raw_url:string}[]>([])
const tab=ref('send'),busy=ref(false),mode=ref('active'),keyboardMode=ref('none'),layout=ref(''),fileKind=ref(1),file=ref<File>(),remote=ref(''),direct=ref(false),privateReply=ref(false)
const sendPrivateReply=computed(()=>mode.value==='reply'&&privateReply.value)
const form=reactive({kind:'text',text:'',title:'',description:'',url:'',picture:'',keyboard:'',reference:'',event:'',reply:'',asset:'',seconds:10})
function choose(e:Event){file.value=(e.target as HTMLInputElement).files?.[0]}
function error(e:unknown){ElMessage.error(e instanceof Error?e.message:'操作失败')}
async function load(){try{[options.value,assets.value]=await Promise.all([request<typeof options.value>({url:base+'options'}),request<typeof assets.value>({url:base+'assets'})])}catch(e){error(e)}}
async function send(){if(busy.value)return;busy.value=true;try{
 if((mode.value==='reply'&&!form.reply)||(mode.value==='event'&&!form.event))throw Error('请选择回复目标')
 const data={...form,reply:mode.value==='reply'?form.reply:'',event:mode.value==='event'?form.event:'',asset:form.kind==='media'?form.asset:'',reference:sendPrivateReply.value?'':form.reference,privateReply:sendPrivateReply.value,wakeup:mode.value==='wakeup',keyboard:keyboardMode.value==='template'?form.keyboard:'',keys:keyboardMode.value==='custom'?JSON.parse(layout.value):undefined}
 await request({url:base+'send',method:'POST',data});ElMessage.success('已加入发送队列');form.text=''
 }catch(e){error(e)}finally{busy.value=false}}
async function upload(){if(busy.value)return;busy.value=true;try{const data=new FormData();data.set('kind',String(fileKind.value));data.set('direct',String(direct.value))
 if(file.value)data.set('file',file.value);else{const url=new URL(remote.value);data.set('url',url.href);data.set('name',url.pathname.split('/').pop()||'QQ 素材')}
 await request({url:base+'upload',method:'POST',data,timeout:600000});ElMessage.success(direct.value?'素材已发送':'素材已上传');await load()
 }catch(e){error(e)}finally{busy.value=false}}
onMounted(load)
</script>
