<template>
  <section class="panel">
    <div class="panel-head"><div><h2>以机器人身份发消息</h2><p>消息会进入发送队列，由 {{ row.botName }} 发到这个{{ privateChat ? '私聊' : '群' }}。</p></div></div>
    <el-form label-position="top" class="panel-body">
      <div class="form-grid">
        <el-form-item label="消息类型">
          <el-select v-model="form.kind"><el-option v-for="v in kinds" :key="v[0]" :value="v[0]" :label="v[1]" /></el-select>
        </el-form-item>
        <el-form-item label="发送方式">
          <el-select v-model="mode">
            <el-option label="主动发送" value="active" />
            <el-option label="回复收到的消息" value="reply" />
            <el-option label="回复 QQ 事件" value="event" />
            <el-option v-if="privateChat" label="互动召回" value="wakeup" />
          </el-select>
        </el-form-item>
      </div>

      <el-form-item v-if="['text', 'markdown'].includes(form.kind)" :label="form.kind === 'markdown' ? 'Markdown 正文' : '消息正文'">
        <el-input v-model="form.text" type="textarea" :rows="5" placeholder="输入要发送的内容" />
      </el-form-item>
      <div v-if="form.kind === 'card'" class="form-grid">
        <el-form-item label="卡片标题"><el-input v-model="form.title" maxlength="50" /></el-form-item>
        <el-form-item label="卡片描述"><el-input v-model="form.description" maxlength="200" /></el-form-item>
        <el-form-item label="跳转地址（HTTPS）"><el-input v-model="form.url" placeholder="https://" /></el-form-item>
        <el-form-item label="图片地址（HTTPS）"><el-input v-model="form.picture" placeholder="https://" /></el-form-item>
      </div>
      <el-form-item v-if="form.kind === 'typing'" label="显示“对方正在输入”的秒数"><el-input-number v-model="form.seconds" :min="1" :max="60" /></el-form-item>
      <el-form-item v-if="form.kind === 'media'" label="选择已上传的素材">
        <el-select v-model="form.asset" placeholder="先在下方上传素材"><el-option v-for="a in assets" :key="a.id" :label="a.filename || a.file_uuid" :value="a.id" /></el-select>
      </el-form-item>

      <div class="form-grid">
        <el-form-item v-if="mode === 'event'" label="回复哪个 QQ 事件">
          <el-select v-model="form.event" placeholder="选择事件"><el-option v-for="s in options.filter(s => s.kind === 'event')" :key="s.id" :value="s.id" :label="s.label + ' · ' + s.at" /></el-select>
        </el-form-item>
        <el-form-item v-if="mode === 'reply'" label="回复哪条消息">
          <el-select v-model="form.reply" placeholder="选择消息"><el-option v-for="s in options.filter(s => s.passive)" :key="s.id" :value="s.id" :label="s.label + ' · ' + s.at" /></el-select>
          <el-checkbox v-if="!privateChat" v-model="privateReply" style="margin-top: 6px">改为私聊回复这位发送者（需为好友）</el-checkbox>
        </el-form-item>
        <el-form-item v-if="!sendPrivateReply" label="引用一条消息（选填）">
          <el-select v-model="form.reference" clearable placeholder="可引用群友或机器人发过的消息"><el-option v-for="s in options.filter(s => s.quotable)" :key="s.id" :value="s.id" :label="(s.direction === 'outbound' ? '机器人 · ' : '') + s.label + ' · ' + s.at" /></el-select>
        </el-form-item>
        <el-form-item label="内嵌按钮">
          <el-select v-model="keyboardMode"><el-option label="不带按钮" value="none" /><el-option label="QQ 键盘模板" value="template" /><el-option label="自定义按钮布局" value="custom" /></el-select>
        </el-form-item>
      </div>
      <el-form-item v-if="keyboardMode === 'template'" label="键盘模板 ID"><el-input v-model="form.keyboard" style="max-width: 360px" /></el-form-item>
      <el-form-item v-if="keyboardMode === 'custom'" label="按钮布局 JSON"><el-input v-model="layout" type="textarea" :rows="4" class="mono" placeholder='{"rows":[{"buttons":[…]}]}' /></el-form-item>
    </el-form>
    <div class="panel-foot">
      <el-button type="primary" :icon="Promotion" :loading="busy" @click="send">发送消息</el-button>
    </div>
  </section>

  <section class="panel">
    <div class="panel-head"><div><h2>素材</h2><p>图片、视频、语音和文件要先上传到 QQ，再在上面选“素材”类型发送。</p></div></div>
    <el-form label-position="top" class="panel-body">
      <div class="form-grid">
        <el-form-item label="素材类型"><el-select v-model="fileKind"><el-option v-for="(label, n) in ['图片', '视频', '语音', '文件']" :key="n" :label="label" :value="n + 1" /></el-select></el-form-item>
        <el-form-item label="来源">
          <div class="form-row" style="width: 100%">
            <el-upload :auto-upload="false" :show-file-list="false" :on-change="choose"><el-button :icon="Upload">{{ file ? '换一个文件' : '选择本地文件' }}</el-button></el-upload>
            <el-input v-if="!file" v-model="remote" placeholder="或填写 HTTPS 文件地址" class="grow" />
            <span v-else class="grow truncate" style="line-height: 34px">{{ file.name }} <el-button link @click="file = undefined">移除</el-button></span>
          </div>
        </el-form-item>
      </div>
      <div class="fact-row" style="align-items: center">
        <el-checkbox v-model="direct">上传后直接发到这个会话</el-checkbox>
        <span class="spacer" />
        <el-button :loading="busy" :disabled="!file && !remote.trim()" @click="upload">{{ direct ? '上传并发送' : '上传素材' }}</el-button>
      </div>
    </el-form>
    <el-table :data="assets" max-height="320" empty-text="还没有上传过素材">
      <el-table-column label="素材" min-width="220"><template #default="{ row }"><span class="truncate">{{ row.filename || row.file_uuid }}</span></template></el-table-column>
      <el-table-column label="有效期" width="180"><template #default="{ row }">{{ row.expires_at || '长期有效' }}</template></el-table-column>
      <el-table-column width="90"><template #default="{ row }"><a v-if="row.raw_url?.startsWith('https://')" :href="row.raw_url" target="_blank" rel="noreferrer">下载</a></template></el-table-column>
    </el-table>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Promotion, Upload } from '@element-plus/icons-vue'
import { ElMessage, type UploadFile } from 'element-plus'
import { request } from '@/api/client'
import type { Conversation } from '@/types'

const props = defineProps<{ row: Conversation }>()
const base = '/conversations/' + props.row.id + '/delivery/'
const privateChat = props.row.type === 'c2c'
const kinds = [['text', '文本'], ['markdown', 'Markdown'], ['card', '图文卡片'], ['media', '素材'], ...(privateChat ? [['typing', '输入中状态']] : [])]
const options = ref<{ id: string; kind: string; direction: string; passive: boolean; quotable: boolean; label: string; at: string }[]>([])
const assets = ref<{ id: string; filename: string; file_uuid: string; expires_at: string; raw_url: string }[]>([])
const busy = ref(false), mode = ref('active'), keyboardMode = ref('none'), layout = ref(''), fileKind = ref(1), file = ref<File>(), remote = ref(''), direct = ref(false), privateReply = ref(false)
const sendPrivateReply = computed(() => mode.value === 'reply' && privateReply.value)
const form = reactive({ kind: 'text', text: '', title: '', description: '', url: '', picture: '', keyboard: '', reference: '', event: '', reply: '', asset: '', seconds: 10 })

function choose(upload: UploadFile) { file.value = upload.raw }
function error(e: unknown) { ElMessage.error(e instanceof Error ? e.message : '操作失败') }
async function load() {
  try { [options.value, assets.value] = await Promise.all([request<typeof options.value>({ url: base + 'options' }), request<typeof assets.value>({ url: base + 'assets' })]) }
  catch (e) { error(e) }
}
async function send() {
  if (busy.value) return
  busy.value = true
  try {
    if ((mode.value === 'reply' && !form.reply) || (mode.value === 'event' && !form.event)) throw Error('请选择回复目标')
    const data = { ...form, reply: mode.value === 'reply' ? form.reply : '', event: mode.value === 'event' ? form.event : '', asset: form.kind === 'media' ? form.asset : '', reference: sendPrivateReply.value ? '' : form.reference, privateReply: sendPrivateReply.value, wakeup: mode.value === 'wakeup', keyboard: keyboardMode.value === 'template' ? form.keyboard : '', keys: keyboardMode.value === 'custom' ? JSON.parse(layout.value) : undefined }
    await request({ url: base + 'send', method: 'POST', data })
    ElMessage.success('已加入发送队列')
    form.text = ''
  } catch (e) { error(e) } finally { busy.value = false }
}
async function upload() {
  if (busy.value) return
  busy.value = true
  try {
    const data = new FormData()
    data.set('kind', String(fileKind.value))
    data.set('direct', String(direct.value))
    if (file.value) data.set('file', file.value)
    else { const url = new URL(remote.value); data.set('url', url.href); data.set('name', url.pathname.split('/').pop() || 'QQ 素材') }
    await request({ url: base + 'upload', method: 'POST', data, timeout: 600000 })
    ElMessage.success(direct.value ? '素材已发送' : '素材已上传')
    file.value = undefined
    remote.value = ''
    await load()
  } catch (e) { error(e) } finally { busy.value = false }
}
onMounted(load)
</script>
