<template>
  <div class="content-page">
    <PageHeader title="模型" description="机器人用到的 AI 模型。DeepSeek、智谱 GLM、TypeSafe 官方接口只需填 API Key，也支持任何 OpenAI 兼容接口。改完可以点“测试”确认能用。"><el-button type="primary" :icon="Plus" @click="openCreate('chat')">添加模型</el-button></PageHeader>
    <template v-for="group in groups" :key="group.kind">
      <div class="list-group-title"><h2>{{ group.title }}</h2><span>{{ group.hint }}</span></div>
      <div v-if="group.items.length" class="entity-list">
        <div v-for="model in group.items" :key="model.id" class="entity-row">
          <div class="entity-name"><span class="entity-icon" :class="model.kind === 'chat' ? 'purple' : 'green'"><el-icon><component :is="model.kind === 'chat' ? ChatDotSquare : model.kind === 'image' ? PictureIcon : model.kind === 'decision' ? Finished : Coin" /></el-icon></span><div><strong>{{ model.name }} <StatusBadge :status="model.status" /><span v-if="model.provider !== 'compatible'" class="pill busy proxy-pill">{{ providerLabel(model.provider, presets) }}</span><span v-if="model.proxyId" class="pill idle proxy-pill" :title="'请求经代理发出：' + proxyName(model.proxyId)">代理 · {{ proxyName(model.proxyId) }}</span></strong><small :title="model.baseUrl">{{ model.baseUrl }}</small></div></div>
          <div class="entity-facts">
            <div><span>模型</span><strong>{{ model.model }}</strong></div>
            <div v-if="model.kind === 'chat'"><span>联网搜索 / 思考等级</span><strong>{{ searchModeText(model.webSearchMode) }} · {{ reasoningText(model.reasoningEffort) }}</strong></div>
            <div v-else-if="model.kind === 'decision'"><span>用途</span><strong>群消息要不要回复</strong></div>
            <div v-else-if="model.kind === 'embedding'"><span>向量维度</span><strong>{{ model.dimension || '—' }}</strong></div>
            <div><span>{{ model.kind === 'image' ? '每张价格' : model.kind === 'decision' ? '输入单价' : '输入 / 输出单价' }}</span><strong :class="{ unset: !hasPrice(model) }">{{ priceText(model) }}</strong></div>
            <div><span>最近测试延迟</span><strong>{{ model.latency ? `${model.latency}ms` : '还没测试' }}</strong></div>
          </div>
          <div class="entity-actions"><el-button :icon="VideoPlay" @click="openWorkbench(model)">测试</el-button><el-button type="primary" plain @click="edit(model)">配置</el-button></div>
        </div>
      </div>
      <div v-else class="panel"><EmptyState mood="thinking" :title="`还没有${group.title}`" :description="group.empty"><el-button :icon="Plus" @click="openCreate(group.kind)">添加{{ group.title }}</el-button></EmptyState></div>
    </template>

    <el-dialog v-model="dialog" :title="form.id?'配置模型':'添加模型配置'" width="620px">
      <el-form label-position="top">
        <el-form-item label="接口来源">
          <el-segmented v-model="providerChoice" :options="providerOptions" class="provider-switch" />
          <small class="form-tip" v-if="currentPreset">{{ presetModels('chat').length ? '官方接口，地址和联网方式已预置，只需要填 API Key。' : '只判断群消息要不要回复，不生成文字。地址已预置，只需要填 API Key。' }}<a v-if="currentPreset.keyUrl" :href="currentPreset.keyUrl" target="_blank" rel="noopener">去{{ currentPreset.name.replace(' 官方','') }}获取 Key</a></small>
          <small class="form-tip" v-else>任何 OpenAI 兼容接口，包括第三方中转。</small>
        </el-form-item>
        <div class="form-grid">
          <el-form-item label="配置名称"><el-input v-model="form.name" :placeholder="currentPreset ? `留空则为 ${currentPreset.name} · ${form.model || '模型名'}` : ''" /></el-form-item>
          <el-form-item label="模型类型"><el-select v-model="form.kind"><el-option label="对话模型" value="chat" :disabled="!!currentPreset && !presetModels('chat').length" /><el-option label="Embedding 模型" value="embedding" :disabled="!!currentPreset && !presetModels('embedding').length" /><el-option label="生图模型" value="image" :disabled="!!currentPreset && !presetModels('image').length" /><el-option label="判断模型" value="decision" :disabled="!presetModels('decision').length" /></el-select><small v-if="!presetModels('decision').length" class="form-tip">判断模型目前只支持 TypeSafe。</small></el-form-item>
        </div>
        <el-form-item v-if="!currentPreset" label="Base URL"><el-input v-model="form.baseUrl" placeholder="例如：https://example.com/v1" /><small class="form-tip">OpenAI 兼容地址通常包含 /v1。</small></el-form-item>
        <div class="form-grid">
          <el-form-item label="模型名称">
            <div class="model-pick">
              <el-select v-model="form.model" filterable allow-create default-first-option placeholder="选择或输入模型名" no-data-text="点右侧“获取列表”从接口读取" @change="onPresetModel">
                <el-option-group v-if="presetModels(form.kind).length" label="常用"><el-option v-for="m in presetModels(form.kind)" :key="m.model" :label="m.label" :value="m.model"><span>{{ m.label }}</span><small class="option-sub mono">{{ m.model }}</small></el-option></el-option-group>
                <el-option-group v-if="extraDiscovered.length" :label="`接口返回 · ${discovered.length} 个`"><el-option v-for="id in extraDiscovered" :key="id" :label="id" :value="id"><span class="mono">{{ id }}</span></el-option></el-option-group>
              </el-select>
              <el-button :loading="discovering" :disabled="!canDiscover" @click="discover">获取列表</el-button>
            </div>
            <small class="form-tip">{{ discoverTip }}</small>
          </el-form-item>
          <el-form-item v-if="form.kind==='embedding'" label="向量维度"><el-input-number v-model="form.dimension" :min="128" :max="4096" /></el-form-item>
          <el-form-item v-else-if="form.kind==='chat'" label="思考等级"><el-select v-model="form.reasoningEffort"><el-option label="模型默认" value="default" /><el-option label="关闭（none）" value="none" /><el-option label="最小（minimal）" value="minimal" /><el-option label="低（low）" value="low" /><el-option label="中（medium）" value="medium" /><el-option label="高（high）" value="high" /></el-select><small class="form-tip">仅支持推理等级的模型会生效；不确定时使用“模型默认”。</small></el-form-item>
        </div>
        <el-form-item v-if="currentPreset && form.kind==='chat'" label="联网搜索">
          <div class="switch-line"><el-switch v-model="officialSearch" :disabled="!presetSupportsSearch" /><span>{{ presetSupportsSearch ? (officialSearch ? `使用${currentPreset.name.replace(' 官方','')}官方联网工具，由模型自己决定什么时候搜索` : '关闭') : '这个模型不支持官方联网' }}</span></div>
        </el-form-item>
        <el-form-item label="API Key"><el-input v-model="apiKey" type="password" show-password :placeholder="form.id?'留空则不覆盖现有密钥':'请输入 API Key'" /><small class="form-tip">密钥保存后仅展示脱敏值。</small></el-form-item>
        <el-form-item label="网络"><el-select v-model="proxyChoice"><el-option label="直连（不走代理）" value="direct" /><el-option v-for="p in proxies" :key="p.id" :label="p.name" :value="p.id"><span>{{ p.name }}</span><small class="option-sub mono">{{ p.address }}</small></el-option></el-select><small class="form-tip">海外模型直连访问不了时选择代理。<el-button link type="primary" size="small" @click="$router.push('/system')">{{ proxies.length ? '管理代理' : '还没有代理，去系统状态添加' }}</el-button></small></el-form-item>
        <el-form-item v-if="form.kind==='image'" label="每张价格（元）" class="price-item"><el-input-number v-model="form.pricePerImage" :min="0" :precision="4" :step="0.01" :controls="false" placeholder="未设置" class="price-single" /><small class="form-tip">每成功生成一张图按这个价格计入成本。</small></el-form-item>
        <el-form-item v-else label="价格（元 / 百万 token）" class="price-item"><div class="price-grid" :class="{ two: form.kind==='embedding', one: form.kind==='decision' }"><label v-for="p in shownPriceFields" :key="p.key"><span>{{ p.label }}</span><el-input-number v-model="form[p.key]" :min="0" :precision="4" :step="0.1" :controls="false" placeholder="未设置" /></label></div><small v-if="form.kind==='decision'" class="form-tip">TypeSafe 只按输入计费，输出免费。官方按美元标价，填写时换算成人民币。</small><small v-else class="form-tip">按实际付的价格填写。成本 = 已记录的用量 × 当前价格，改价后历史成本会一起按新价格重算。</small></el-form-item>
        <template v-if="form.kind==='chat'">
          <el-form-item v-if="!currentPreset" label="模型内置联网搜索"><el-select v-model="form.webSearchMode"><el-option label="关闭" value="disabled" /><el-option label="Responses API（/responses + web_search，推荐）" value="responses" /><el-option label="Qwen / DashScope（enable_search）" value="qwen" /><el-option label="Chat Completions（web_search_options）" value="openai" /><el-option label="自定义参数" value="custom" /></el-select><small class="form-tip">Responses 模式会调用 /v1/responses，并发送 tools: [{ type: 'web_search' }]。对话测试和机器人回答使用同一配置。</small></el-form-item>
          <el-form-item label="额外请求参数 JSON"><el-input v-model="extraBodyText" type="textarea" :rows="currentPreset ? 2 : 4" placeholder='例如：{"temperature":0.2}' /><small class="form-tip">可留空。思考等级由上方设置统一生成，核心请求字段不会被覆盖。</small></el-form-item>
        </template>
      </el-form>
      <template #footer><el-button @click="dialog=false">取消</el-button><el-button type="primary" @click="save">保存配置</el-button></template>
    </el-dialog>

    <el-dialog v-model="workbenchVisible" title="模型测试工作台" width="720px" class="model-workbench">
      <template v-if="testingModel">
        <div class="workbench-model"><span class="entity-icon purple"><el-icon><MagicStick /></el-icon></span><div><strong>{{ testingModel.name }}</strong><span>{{ testingModel.model }} · {{ kindLabel(testingModel.kind) }} · {{ providerLabel(testingModel.provider, presets) }}</span></div></div>
        <el-segmented v-if="testingModel.kind === 'chat'" v-model="testMode" :options="testModeOptions" class="test-mode" @change="onTestModeChange" />
        <el-alert :title="`请求 endpoint：${testEndpoint}`" type="info" :closable="false" show-icon class="endpoint-alert" />
        <el-form label-position="top">
          <el-form-item v-if="testingModel.kind === 'chat' && testMode === 'chat'" label="System Prompt（可选）"><el-input v-model="testForm.systemPrompt" type="textarea" :rows="2" placeholder="例如：你是一名简洁、准确的客服助手" /></el-form-item>
          <el-form-item :label="inputLabel"><el-input v-model="testForm.input" type="textarea" :rows="testMode === 'web' ? 2 : 4" :placeholder="testingModel.kind === 'chat' ? '输入要发送给模型的消息' : testingModel.kind === 'decision' ? '输入一条群消息，看它判断要不要回复' : '输入需要生成向量的文本'" /></el-form-item>
          <el-button type="primary" :loading="testing" :disabled="!testForm.input.trim()" @click="runTest">{{ testMode === 'web' ? '运行联网测试' : '运行测试' }}</el-button>
        </el-form>
        <el-alert v-if="testError" title="测试失败" type="error" :closable="false" show-icon class="test-error"><template #default><pre>{{ testError }}</pre><el-button size="small" :icon="CopyDocument" @click="copyText(testError)">复制错误</el-button></template></el-alert>
        <section v-if="testResult" class="workbench-result">
          <div class="result-heading"><h3>测试结果 · {{ testResult.model }}</h3><StatusBadge status="success" text="请求成功" /></div>
          <template v-if="testResult.kind === 'chat'">
            <div v-if="testMode === 'web'" class="search-trace" :class="{ hit: testResult.searched }">
              <div class="search-line"><el-icon><Search /></el-icon><strong>{{ testResult.searched ? `已联网${testResult.searchQueries?.length ? ` · 搜索 ${testResult.searchQueries.length} 次` : ''}${testResult.sources?.length ? ` · 引用 ${testResult.sources.length} 个来源` : ''}` : '这次没有联网' }}</strong><span v-if="!testResult.searched">模型认为不需要搜索，或联网没有生效。换一个需要实时信息的问题再试。</span></div>
              <div v-if="testResult.searchQueries?.length" class="search-queries"><span v-for="q in testResult.searchQueries" :key="q" class="pill idle">{{ q }}</span></div>
              <ol v-if="testResult.sources?.length" class="search-sources"><li v-for="src in testResult.sources" :key="src.url"><a :href="src.url" target="_blank" rel="noopener noreferrer">{{ src.title || src.url }}</a><small>{{ src.siteName || hostOf(src.url) }}</small></li></ol>
            </div>
            <div class="result-output">{{ testResult.content }}</div>
            <div class="result-stats"><div><span>耗时</span><strong>{{ testResult.latencyMs }}ms</strong></div><div><span>输入 Token</span><strong>{{ testResult.inputTokens }}</strong></div><div><span>输出 Token</span><strong>{{ testResult.outputTokens }}</strong></div></div>
          </template>
          <template v-else-if="testResult.kind === 'decision'">
            <ul class="verdicts">
              <li v-for="v in verdicts(testResult.decision)" :key="v.label" :class="{ on: v.on }"><span class="verdict-mark" aria-hidden="true"><el-icon><component :is="v.on ? Select : SemiSelect" /></el-icon></span><span class="verdict-label">{{ v.label }}</span><strong>{{ v.text }}</strong></li>
            </ul>
            <details class="raw-json"><summary>原始返回</summary><pre>{{ prettyJSON(testResult.content) }}</pre></details>
            <div class="result-stats"><div><span>耗时</span><strong>{{ testResult.latencyMs }}ms</strong></div><div><span>输入 Token</span><strong>{{ testResult.inputTokens }}</strong></div><div><span>输出 Token</span><strong>{{ testResult.outputTokens }}</strong></div></div>
          </template>
          <template v-else-if="testResult.kind === 'image'"><el-image v-if="testResult.imageUrl" :src="testResult.imageUrl" :preview-src-list="[testResult.imageUrl]" fit="contain" class="image-result" /><div class="result-stats"><div><span>耗时</span><strong>{{ testResult.latencyMs }}ms</strong></div></div></template>
          <template v-else><div class="result-stats"><div><span>耗时</span><strong>{{ testResult.latencyMs }}ms</strong></div><div><span>向量维度</span><strong>{{ testResult.dimensions }}</strong></div><div><span>预览项数</span><strong>{{ Math.min(12, testResult.vectorPreview.length) }}</strong></div></div><pre class="vector-preview">[{{ testResult.vectorPreview.slice(0, 12).map(value => Number(value).toFixed(6)).join(', ') }}{{ testResult.vectorPreview.length > 12 ? ', …' : '' }}]</pre></template>
        </section>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ChatDotSquare, Coin, CopyDocument, Finished, MagicStick, Picture as PictureIcon, Plus, Search, Select, SemiSelect, VideoPlay } from '@element-plus/icons-vue'
import { modelProviderPresets, officialSearchModes, providerLabel } from '@/utils/model-providers'
import EmptyState from '@/components/EmptyState.vue'
import { ElMessage } from 'element-plus'
import { api, sanitizeServiceError } from '@/api'
import PageHeader from '@/components/PageHeader.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import type { ModelDecision, ModelKind, ModelProfile, ModelProviderId, ModelProviderPreset, ModelProxy, ModelTestResult } from '@/types'

const models=ref<ModelProfile[]>([]),dialog=ref(false),apiKey=ref(''),extraBodyText=ref(''),workbenchVisible=ref(false),testing=ref(false),testingModel=ref<ModelProfile>(),testResult=ref<ModelTestResult>(),testError=ref('')
const form=reactive<Partial<ModelProfile>>({});
type PriceKey='inputPricePerMillion'|'outputPricePerMillion'|'cacheReadPricePerMillion'|'cacheWritePricePerMillion'
const priceFields:Array<{key:PriceKey;label:string}>=[{key:'inputPricePerMillion',label:'输入'},{key:'outputPricePerMillion',label:'输出'},{key:'cacheReadPricePerMillion',label:'缓存读取'},{key:'cacheWritePricePerMillion',label:'缓存写入'}]
const shownPriceFields=computed(()=>form.kind==='decision'?priceFields.slice(0,1):form.kind==='embedding'?priceFields.slice(0,2):priceFields)
function kindLabel(kind:ModelKind){return ({chat:'对话',embedding:'Embedding',image:'生图',decision:'判断'} as Record<ModelKind,string>)[kind]}
function verdicts(d:ModelDecision){return [{label:'要不要回复',on:d.reply,text:d.reply?'需要回复':'不用回复'},{label:'只是寒暄',on:d.socialOnly,text:d.socialOnly?'是':'不是'},{label:'需要联网查',on:d.needsWebSearch,text:d.needsWebSearch?'需要':'不需要'}]}
function prettyJSON(text:string){try{return JSON.stringify(JSON.parse(text),null,2)}catch{return text}}
function hasPrice(m:ModelProfile){return m.kind==='image'?m.pricePerImage!=null:m.inputPricePerMillion!=null||m.outputPricePerMillion!=null}
function priceText(m:ModelProfile){if(!hasPrice(m))return '未设置';if(m.kind==='image')return `¥${m.pricePerImage} / 张`;if(m.kind==='decision')return m.inputPricePerMillion==null?'未设置':`¥${m.inputPricePerMillion}`;const f=(v?:number|null)=>v==null?'—':'¥'+v;return `${f(m.inputPricePerMillion)} / ${f(m.outputPricePerMillion)}`}
const groups=computed(()=>[{kind:'chat' as const,title:'对话模型',hint:'用来生成回复',empty:'至少需要一个对话模型，机器人才能回复消息。',items:models.value.filter(m=>m.kind==='chat')},{kind:'decision' as const,title:'判断模型',hint:'只判断群消息要不要回复，比对话模型快、便宜',empty:'可选。添加 TypeSafe 后，到智能体设置的“群消息判断模型”里选择。',items:models.value.filter(m=>m.kind==='decision')},{kind:'embedding' as const,title:'Embedding 模型',hint:'用来给知识库建索引、检索相关内容',empty:'知识库需要 Embedding 模型才能建索引。',items:models.value.filter(m=>m.kind==='embedding')},{kind:'image' as const,title:'生图模型',hint:'“生成图片”技能使用，在每个 AI 客服里选择用哪一个',empty:'添加后，到 AI 客服的“生图模型”里选择。',items:models.value.filter(m=>m.kind==='image')}]);const testForm=reactive({input:'',systemPrompt:''});const testEndpoint=computed(()=>{if(!testingModel.value)return '';const base=testingModel.value.baseUrl.replace(/\/$/,'');if(testingModel.value.kind==='decision')return `${base}/systemone`;if(testingModel.value.provider==='deepseek'||testingModel.value.provider==='zhipu')return `${base}/${testingModel.value.kind==='embedding'?'embeddings':testingModel.value.kind==='image'?'images/generations':'chat/completions'}`;if(testingModel.value.kind==='embedding')return `${base}/embeddings`;if(testingModel.value.kind==='image')return `${base}/images/generations`;return `${base}/${testingModel.value.webSearchMode==='responses'?'responses':'chat/completions'}`})
const proxies=ref<ModelProxy[]>([])
const presets=ref<ModelProviderPreset[]>(modelProviderPresets)
const providerOptions=computed(()=>[...presets.value.map(p=>({label:p.name,value:p.id})),{label:'兼容接口',value:'compatible'}])
const currentPreset=computed(()=>presets.value.find(p=>p.id===form.provider))
function presetModels(kind?:ModelKind){return currentPreset.value?.models.filter(m=>m.kind===kind)||[]}
const presetSupportsSearch=computed(()=>{const m=presetModels('chat').find(x=>x.model===form.model);return m?m.supportsWebSearch!==false:true})
const officialSearch=computed({get:()=>officialSearchModes.includes(form.webSearchMode!),set:(on:boolean)=>{form.webSearchMode=on&&currentPreset.value?currentPreset.value.webSearchMode:'disabled'}})
function applyPreset(){const p=currentPreset.value;if(!p)return;form.baseUrl=p.baseUrl;const list=presetModels(form.kind);if(!list.some(m=>m.model===form.model))form.model=list[0]?.model||'';onPresetModel()}
function onPresetModel(){const p=currentPreset.value;if(!p)return;const m=presetModels(form.kind).find(x=>x.model===form.model);if(form.kind==='embedding'&&m?.dimension)form.dimension=m.dimension;if(form.kind==='chat')form.webSearchMode=m?.supportsWebSearch===false?'disabled':(officialSearchModes.includes(form.webSearchMode!)||!form.id?p.webSearchMode:form.webSearchMode)}
const providerChoice=computed({get:()=>(form.provider||'compatible') as ModelProviderId,set:(v:ModelProviderId)=>{const was=form.provider;form.provider=v;if(v==='compatible'){if(form.kind==='decision')form.kind='chat';if(was!=='compatible'){form.baseUrl='';form.model='';if(officialSearchModes.includes(form.webSearchMode!))form.webSearchMode='disabled'}}else{if(currentPreset.value&&!presetModels(form.kind).length)form.kind=currentPreset.value.models[0]?.kind||'chat';applyPreset()}}})
watch(()=>form.kind,()=>{if(currentPreset.value&&dialog.value)applyPreset()})
const testMode=ref<'chat'|'web'>('chat')
const testModeOptions=computed(()=>[{label:'普通对话',value:'chat'},{label:'联网测试',value:'web',disabled:testingModel.value?.webSearchMode==='disabled'}])
const webPrompt='今天有哪些值得关注的科技新闻？请列出 3 条并注明来源。'
function onTestModeChange(){testResult.value=undefined;testError.value='';testForm.input=testMode.value==='web'?webPrompt:'请用一句话介绍你自己。'}
const inputLabel=computed(()=>{const m=testingModel.value;if(!m)return '';if(m.kind==='image')return '画面描述（测试会真实生成一张图，可能产生费用）';if(m.kind==='embedding')return '测试文本';if(m.kind==='decision')return '群消息';return testMode.value==='web'?'需要实时信息的问题':'用户消息'})
function hostOf(url:string){try{return new URL(url).hostname}catch{return url}}
const discovered=ref<string[]>([]),discovering=ref(false),discoverError=ref('')
const extraDiscovered=computed(()=>{const known=new Set(presetModels(form.kind).map(m=>m.model));return discovered.value.filter(id=>!known.has(id))})
const canDiscover=computed(()=>(!!form.id||!!apiKey.value.trim())&&(!!currentPreset.value||!!form.baseUrl?.trim()))
const discoverTip=computed(()=>discoverError.value||(discovered.value.length?`已从接口读到 ${discovered.value.length} 个模型，也可以直接输入。`:canDiscover.value?'可从列表选，也可以直接输入模型名。':currentPreset.value?'填好 API Key 后可获取可用模型，也可以直接输入。':'填好 Base URL 和 API Key 后可获取可用模型，也可以直接输入。'))
watch(()=>[form.provider,form.baseUrl,form.proxyId],()=>{discovered.value=[];discoverError.value=''})
async function discover(){discovering.value=true;discoverError.value='';try{const key=apiKey.value.trim();const list=await api.models.discover({provider:(form.provider||'compatible') as ModelProviderId,baseUrl:currentPreset.value?undefined:form.baseUrl?.trim(),apiKey:key||undefined,profileId:form.id||undefined,proxyId:form.proxyId||''});discovered.value=[...new Set(list)].sort();if(!list.length)discoverError.value='接口没有返回模型，可以直接输入模型名。'}catch(error){discovered.value=[];discoverError.value=sanitizeServiceError(error instanceof Error?error.message:'')||'获取失败，可以直接输入模型名。'}finally{discovering.value=false}}
const proxyChoice=computed({get:()=>form.proxyId||'direct',set:(v:string)=>{form.proxyId=v==='direct'?null:v}})
function proxyName(id?:string|null){if(!id)return '直连';return proxies.value.find(p=>p.id===id)?.name||'代理已失效'}
onMounted(load);async function load(){const [list,proxyList,presetList]=await Promise.allSettled([api.models.list(),api.system.proxies.list(),api.models.presets()]);if(presetList.status==='fulfilled')presets.value=presetList.value;if(list.status==='fulfilled')models.value=list.value;else ElMessage.error(list.reason instanceof Error?list.reason.message:'模型读取失败');if(proxyList.status==='fulfilled')proxies.value=proxyList.value};function searchModeText(mode:ModelProfile['webSearchMode']){return ({disabled:'关闭',responses:'Responses Web Search',qwen:'Qwen 内置搜索',openai:'Chat Completions Web Search',custom:'自定义参数',deepseek:'DeepSeek 官方联网',zhipu:'智谱官方联网'} as Record<string,string>)[mode]||'关闭'};function reasoningText(level:ModelProfile['reasoningEffort']){return ({default:'模型默认',none:'关闭',minimal:'最小',low:'低',medium:'中',high:'高'} as Record<string,string>)[level]||'模型默认'};function openCreate(kind:ModelKind='chat'){const decisionPreset=kind==='decision'?presets.value.find(p=>p.models.some(m=>m.kind==='decision')):undefined;Object.assign(form,{id:'',name:'',kind,provider:decisionPreset?.id||'compatible',model:kind==='image'?'gpt-image-2.6-flare':decisionPreset?.models.find(m=>m.kind==='decision')?.model||'',baseUrl:decisionPreset?.baseUrl||'',dimension:1024,status:'online',webSearchMode:'disabled',reasoningEffort:'default',extraBody:{},inputPricePerMillion:null,outputPricePerMillion:null,cacheReadPricePerMillion:null,cacheWritePricePerMillion:null,pricePerImage:null,proxyId:null});apiKey.value='';extraBodyText.value='';discovered.value=[];discoverError.value='';dialog.value=true};function edit(m:ModelProfile){Object.assign(form,m);apiKey.value='';extraBodyText.value=Object.keys(m.extraBody||{}).length?JSON.stringify(m.extraBody,null,2):'';discovered.value=[];discoverError.value='';dialog.value=true};async function save(){try{form.extraBody=extraBodyText.value.trim()?JSON.parse(extraBodyText.value):{}}catch{ElMessage.error('额外请求参数必须是有效的 JSON 对象');return}if(Array.isArray(form.extraBody)||form.extraBody===null||typeof form.extraBody!=='object'){ElMessage.error('额外请求参数必须是 JSON 对象');return}await api.models.save(form,apiKey.value);await load();dialog.value=false;ElMessage.success('模型配置已保存')}
function openWorkbench(model:ModelProfile){testingModel.value=model;testForm.input=model.kind==='chat'?'请用一句话介绍你自己。':model.kind==='decision'?'小Q在吗？我的订单一直显示处理中，帮忙看下':model.kind==='image'?'一只坐在窗台上的橘猫，午后阳光':'这是一段用于测试向量模型的文本。';testForm.systemPrompt='';testMode.value='chat';testResult.value=undefined;testError.value='';workbenchVisible.value=true}
async function runTest(){if(!testingModel.value||!testForm.input.trim())return;testing.value=true;testResult.value=undefined;testError.value='';try{const result=await api.models.test(testingModel.value.id,{input:testForm.input.trim(),systemPrompt:testingModel.value.kind==='chat'&&testMode.value==='chat'&&testForm.systemPrompt.trim()?testForm.systemPrompt.trim():undefined,webSearch:testingModel.value.kind==='chat'&&testMode.value==='web'?true:undefined});testResult.value=result;testingModel.value.latency=result.latencyMs;testingModel.value.status='online'}catch(error){testingModel.value.status='warning';testError.value=sanitizeServiceError(error instanceof Error?error.message:'模型测试失败')||'模型测试失败'}finally{testing.value=false}}
async function copyText(value:string){await navigator.clipboard?.writeText(value);ElMessage.success('错误信息已复制')}
useAdminEvents(['model_profiles','model_proxies'], load)
</script>

<style scoped>
.price-item :deep(.el-form-item__content) { display: block; }
.proxy-pill { margin-left: 6px; vertical-align: 1px; }
.option-sub { margin-left: 8px; color: var(--muted); font-size: 12px; }
.price-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; width: 100%; }
.price-grid label { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.price-grid span { color: var(--muted); font-size: 12px; }
.price-grid .el-input-number { width: 100%; }
.price-grid.two { grid-template-columns: repeat(2, minmax(0, 1fr)); max-width: 50%; }
.price-single { width: 200px; }
.price-grid.one { grid-template-columns: 1fr; max-width: 25%; }
.verdicts { display: grid; gap: 6px; margin: 0 0 10px; padding: 0; list-style: none; }
.verdicts li { display: flex; align-items: center; gap: 10px; padding: 8px 12px; border: 1px solid var(--line); border-radius: var(--radius-sm); background: var(--surface-2); transition: border-color .2s, background-color .2s; }
.verdicts li.on { border-color: var(--primary-light); background: var(--primary-light); }
.verdict-mark { display: inline-flex; width: 20px; height: 20px; align-items: center; justify-content: center; border-radius: 50%; background: var(--surface-3); color: var(--muted); font-size: 12px; }
.verdicts li.on .verdict-mark { background: var(--primary); color: #fff; }
.verdict-label { flex: 1; color: var(--text-2); font-size: 13px; }
.verdicts li.on strong { color: var(--primary-text); }
.raw-json { margin-bottom: 10px; font-size: 13px; }
.raw-json summary { cursor: pointer; color: var(--muted); }
.raw-json pre { margin: 6px 0 0; padding: 10px; max-height: 200px; overflow: auto; border-radius: var(--radius-sm); background: var(--surface-2); font-size: 12px; white-space: pre-wrap; overflow-wrap: anywhere; }
@media (max-width: 640px) { .price-grid.one { max-width: 50%; } }
.entity-facts strong.unset { color: var(--muted); font-weight: 400; }
@media (max-width: 640px) { .price-grid { grid-template-columns: 1fr 1fr; } }
.provider-switch { width: 100%; }
.model-pick { display: flex; gap: 8px; width: 100%; }
.model-pick .el-select { flex: 1; min-width: 0; }
.switch-line { display: flex; align-items: center; gap: 10px; color: var(--muted); font-size: 13px; }
.test-mode { margin-bottom: 12px; }
.search-trace { margin-bottom: 12px; padding: 10px 12px; border: 1px solid var(--line); border-radius: var(--radius-sm); background: var(--surface-2); }
.search-trace.hit { border-color: var(--primary-light); }
.search-line { display: flex; flex-wrap: wrap; align-items: center; gap: 6px 8px; }
.search-line .el-icon { color: var(--muted); }
.search-trace.hit .search-line .el-icon { color: var(--primary); }
.search-line span { color: var(--muted); font-size: 12px; }
.search-queries { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
.search-sources { margin: 8px 0 0; padding-left: 20px; max-height: 160px; overflow: auto; font-size: 13px; line-height: 1.6; }
.search-sources a { color: var(--primary-text); text-decoration: none; overflow-wrap: anywhere; }
.search-sources a:hover { text-decoration: underline; }
.search-sources small { margin-left: 8px; color: var(--muted); font-size: 12px; }
.image-result { display: block; width: 100%; max-height: 360px; margin-bottom: 12px; border-radius: 8px; background: var(--surface-2); }
</style>
