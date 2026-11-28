<template>
  <div ref="pageRef" class="content-page">
    <PageHeader title="模型与代理" description="配置并测试对话、群判断、Embedding 与生图模型；内置网络代理中继管理，海外模型不再来回跳页。">
      <el-segmented v-model="activeTab" :options="tabOptions" @change="syncTabQuery" />
      <el-button @click="showTopology = !showTopology">{{ showTopology ? '收起 3D 拓扑' : '展开 3D 拓扑' }}</el-button>
      <el-button v-if="activeTab === 'models'" type="primary" :icon="Plus" @click="openCreate('chat')">添加模型</el-button>
      <el-button v-else type="primary" :icon="Plus" @click="openProxy()">添加代理</el-button>
    </PageHeader>

    <section v-if="showTopology" class="topo-stage">
      <ModelTopology3D :models="models" :proxies="proxies" :proxy-tests="proxyTests" @select-model="openWorkbench" @select-proxy="onSelectProxyFrom3D" />
      <div class="topo-hud">
        <span class="holo-badge"><i /><span>MODEL & PROXY MESH</span></span>
        <div class="holo-kpi-row">
          <span class="holo-kpi is-cyan">模型总数 <strong>{{ models.length }}</strong></span>
          <span class="holo-kpi is-green">在线 <strong>{{ models.filter(m => m.status === 'online').length }}</strong></span>
          <span class="holo-kpi">代理线路 <strong>{{ proxies.length }}</strong></span>
        </div>
        <span class="topo-tip">点击 3D 模型球可直达测试台 · 点击代理水晶可管理代理</span>
      </div>
    </section>

    <!-- Tab 1: 模型配置 -->
    <template v-if="activeTab === 'models'">
      <template v-for="group in groups" :key="group.kind">
        <div class="list-group-title"><h2>{{ group.title }}</h2><span>{{ group.hint }}</span></div>
        <div v-if="group.items.length" class="entity-list">
          <div v-for="model in group.items" :key="model.id" class="entity-row">
            <div class="entity-name">
              <span class="entity-icon" :class="model.kind === 'chat' ? 'purple' : 'green'">
                <el-icon><component :is="model.kind === 'chat' ? ChatDotSquare : model.kind === 'image' ? PictureIcon : model.kind === 'decision' ? Finished : Coin" /></el-icon>
              </span>
              <div>
                <strong>
                  <span>{{ model.name }}</span>
                  <StatusBadge :status="model.status" />
                  <span v-if="model.provider !== 'compatible'" class="pill busy proxy-pill">{{ providerLabel(model.provider, presets) }}</span>
                  <span v-if="model.proxyId" class="pill idle proxy-pill" :title="'请求经代理发出：' + proxyName(model.proxyId)">代理 · {{ proxyName(model.proxyId) }}</span>
                </strong>
                <small :title="model.baseUrl">{{ model.baseUrl }}</small>
              </div>
            </div>
            <div class="entity-facts">
              <div><span>模型</span><strong>{{ model.model }}</strong></div>
              <div v-if="model.kind === 'chat'"><span>联网搜索 / 思考等级</span><strong>{{ searchModeText(model.webSearchMode) }} · {{ reasoningText(model.reasoningEffort) }}</strong></div>
              <div v-else-if="model.kind === 'decision'"><span>用途</span><strong>群消息要不要回复</strong></div>
              <div v-else-if="model.kind === 'embedding'"><span>向量维度</span><strong>{{ model.dimension || '—' }}</strong></div>
              <div><span>{{ model.kind === 'image' ? '每张价格' : model.kind === 'decision' ? '输入单价' : '输入 / 输出单价' }}</span><strong :class="{ unset: !hasPrice(model) }">{{ priceText(model) }}</strong></div>
              <div><span>最近测试延迟</span><strong>{{ model.latency ? `${model.latency}ms` : '还没测试' }}</strong></div>
            </div>
            <div class="entity-actions">
              <el-button :icon="VideoPlay" @click="openWorkbench(model)">测试</el-button>
              <el-button type="primary" plain @click="edit(model)">配置</el-button>
            </div>
          </div>
        </div>
        <div v-else class="panel"><EmptyState mood="thinking" :title="`还没有${group.title}`" :description="group.empty"><el-button :icon="Plus" @click="openCreate(group.kind)">添加{{ group.title }}</el-button></EmptyState></div>
      </template>
    </template>

    <!-- Tab 2: 网络代理管理 -->
    <template v-else>
      <section class="panel cyber-panel proxy-panel">
        <div class="panel-head">
          <div>
            <h2>模型网络代理</h2>
            <p>支持 http://、https://、socks5://。配置后可在任意模型编辑窗口中绑定使用。</p>
          </div>
          <el-button type="primary" :icon="Plus" @click="openProxy()">添加代理</el-button>
        </div>
        <div class="panel-body">
          <div v-if="proxies.length" class="proxy-list">
            <div v-for="p in proxies" :key="p.id" class="proxy-row">
              <span class="proxy-name">
                <strong>{{ p.name }}</strong>
                <small class="proxy-meta">
                  <span class="mono proxy-address" :title="p.address">{{ p.address }}</span>
                  <span v-if="p.username || p.hasPassword" class="proxy-auth">{{ p.username ? `用户 ${p.username}` : '无用户名' }}{{ p.hasPassword ? ' · 已设密码' : '' }}</span>
                </small>
              </span>
              <span class="proxy-result" :title="proxyTests[p.id]?.error || ''"><StatusBadge v-if="proxyTests[p.id]" v-bind="proxyBadge(p.id)" /></span>
              <span class="proxy-actions">
                <el-button size="small" :loading="proxyTests[p.id]?.testing" @click="testProxy(p)">测试连通</el-button>
                <el-button size="small" link type="primary" @click="openProxy(p)">编辑</el-button>
                <el-button size="small" link type="danger" @click="removeProxy(p)">删除</el-button>
              </span>
            </div>
          </div>
          <EmptyState v-else compact :title="proxiesLoaded ? '还没有配置网络代理' : '代理列表读取失败'" :description="proxiesLoaded ? '当前所有模型均直接连接接口。海外模型无法直连时可在此新增代理线路。' : '刷新后重试。'">
            <el-button type="primary" :icon="Plus" @click="openProxy()">添加代理</el-button>
          </EmptyState>
        </div>
      </section>
    </template>

    <!-- 添加/编辑模型弹窗 -->
    <el-dialog v-model="dialog" :title="form.id ? '配置模型' : '添加模型配置'" width="680px">
      <div class="dlg-tabs">
        <el-segmented
          v-model="dialogTab"
          :options="[
            { label: '基础接入', value: 'basic' },
            { label: '能力与网络', value: 'advanced' },
            { label: '计费单价', value: 'pricing' },
          ]"
        />
      </div>
      <el-form label-position="top">
        <div v-show="dialogTab === 'basic'" class="dlg-stack">
          <div class="dlg-section">
            <div class="dlg-section-head"><strong>接口来源与类型</strong><span>选择官方预置或 OpenAI 兼容接口</span></div>
            <el-form-item label="接口来源">
              <el-segmented v-model="providerChoice" :options="providerOptions" class="provider-switch" />
              <small class="form-tip" v-if="currentPreset">
                {{ presetModels('chat').length ? '官方接口，地址和联网方式已预置，只需要填 API Key。' : presetModels('embedding').length ? '图文多模态向量，知识库里的图片和截图可以直接检索。地址已预置，只需要填 API Key。' : '只判断群消息要不要回复，不生成文字。地址已预置，只需要填 API Key。' }}
                <a v-if="currentPreset.keyUrl" :href="currentPreset.keyUrl" target="_blank" rel="noopener">去{{ currentPreset.name.replace(' 官方', '') }}获取 Key</a>
              </small>
              <small class="form-tip" v-else>任何 OpenAI 兼容接口，包括第三方中转。</small>
            </el-form-item>
            <div class="form-grid">
              <el-form-item label="配置名称"><el-input v-model="form.name" :placeholder="currentPreset ? `留空则为 ${currentPreset.name} · ${form.model || '模型名'}` : ''" /></el-form-item>
              <el-form-item label="模型类型">
                <el-select v-model="form.kind">
                  <el-option label="对话模型" value="chat" :disabled="!!currentPreset && !presetModels('chat').length" />
                  <el-option label="Embedding 模型" value="embedding" :disabled="!!currentPreset && !presetModels('embedding').length" />
                  <el-option label="生图模型" value="image" :disabled="!!currentPreset && !presetModels('image').length" />
                  <el-option label="判断模型" value="decision" :disabled="!presetModels('decision').length" />
                </el-select>
                <small v-if="!presetModels('decision').length" class="form-tip">判断模型目前只支持 TypeSafe。</small>
              </el-form-item>
            </div>
            <el-form-item v-if="!currentPreset" label="Base URL"><el-input v-model="form.baseUrl" placeholder="例如：https://example.com/v1" /><small class="form-tip">OpenAI 兼容地址通常包含 /v1。</small></el-form-item>
          </div>

          <div class="dlg-section">
            <div class="dlg-section-head"><strong>模型标识与凭证</strong><span>支持自动拉取接口模型列表</span></div>
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
              <el-form-item v-if="form.kind === 'embedding'" label="向量维度"><el-input-number v-model="form.dimension" :min="128" :max="form.provider === 'google' ? 3072 : 4096" /></el-form-item>
            </div>
            <el-form-item label="API Key"><el-input v-model="apiKey" type="password" show-password :placeholder="form.id ? '留空则不覆盖现有密钥' : '请输入 API Key'" /><small class="form-tip">密钥加密保存，保存后仅展示脱敏值。</small></el-form-item>
          </div>
        </div>

        <div v-show="dialogTab === 'advanced'" class="dlg-stack">
          <div v-if="form.kind === 'chat'" class="dlg-section">
            <div class="dlg-section-head"><strong>对话推理与联网</strong><span>仅对话模型生效</span></div>
            <el-form-item label="思考等级">
              <el-select v-model="form.reasoningEffort">
                <el-option label="模型默认" value="default" /><el-option label="关闭（none）" value="none" />
                <el-option label="最小（minimal）" value="minimal" /><el-option label="低（low）" value="low" />
                <el-option label="中（medium）" value="medium" /><el-option label="高（high）" value="high" />
              </el-select>
              <small class="form-tip">仅支持推理等级的模型会生效；不确定时使用“模型默认”。</small>
            </el-form-item>
            <el-form-item v-if="currentPreset" label="联网搜索">
              <div class="switch-line"><el-switch v-model="officialSearch" :disabled="!presetSupportsSearch" /><span>{{ presetSupportsSearch ? (officialSearch ? `使用${currentPreset.name.replace(' 官方', '')}官方联网工具` : '关闭') : '这个模型不支持官方联网' }}</span></div>
            </el-form-item>
            <el-form-item v-else label="模型内置联网搜索">
              <el-select v-model="form.webSearchMode">
                <el-option label="关闭" value="disabled" /><el-option label="Responses API（推荐）" value="responses" />
                <el-option label="Qwen / DashScope" value="qwen" /><el-option label="Chat Completions" value="openai" /><el-option label="自定义参数" value="custom" />
              </el-select>
            </el-form-item>
            <el-form-item label="额外请求参数 JSON"><el-input v-model="extraBodyText" type="textarea" :rows="3" placeholder='例如：{"temperature":0.2}' /></el-form-item>
          </div>
          <div v-else class="dlg-tip-box">当前所选类型（{{ kindLabel(form.kind || 'embedding') }}）无额外的对话推理/联网参数，可在下方配置网络出口。</div>

          <div class="dlg-section">
            <div class="dlg-section-head"><strong>网络路由出口</strong><span>海外模型不可直连时绑定代理线路</span></div>
            <el-form-item label="代理线路">
              <el-select v-model="proxyChoice">
                <el-option label="直连（不走代理）" value="direct" />
                <el-option v-for="p in proxies" :key="p.id" :label="p.name" :value="p.id"><span>{{ p.name }}</span><small class="option-sub mono">{{ p.address }}</small></el-option>
              </el-select>
              <small class="form-tip">如需新增或测试线路，点此<el-button link type="primary" size="small" @click="goProxyTab">{{ proxies.length ? '在本页管理代理' : '在本页添加代理' }}</el-button></small>
            </el-form-item>
          </div>
        </div>

        <div v-show="dialogTab === 'pricing'" class="dlg-stack">
          <div class="dlg-section">
            <div class="dlg-section-head"><strong>消费估算单价</strong><span>按实际结算价格填写，用于仪表盘实时成本统计</span></div>
            <el-form-item v-if="form.kind === 'image'" label="每张价格（元）" class="price-item"><el-input-number v-model="form.pricePerImage" :min="0" :precision="4" :step="0.01" :controls="false" placeholder="未设置" class="price-single" /></el-form-item>
            <el-form-item v-else label="价格（元 / 百万 token）" class="price-item">
              <div class="price-grid" :class="{ two: form.kind === 'embedding', one: form.kind === 'decision' }">
                <label v-for="p in shownPriceFields" :key="p.key"><span>{{ p.label }}</span><el-input-number v-model="form[p.key]" :min="0" :precision="4" :step="0.1" :controls="false" placeholder="未设置" /></label>
              </div>
            </el-form-item>
          </div>
        </div>
      </el-form>
      <template #footer><el-button @click="dialog = false">取消</el-button><el-button type="primary" @click="save">保存配置</el-button></template>
    </el-dialog>

    <!-- 添加/编辑代理弹窗 -->
    <el-dialog v-model="proxyDialog" :title="proxyForm.id ? '编辑代理' : '添加代理'" width="540px">
      <el-form label-position="top" @submit.prevent>
        <div class="dlg-stack">
          <div class="dlg-section">
            <div class="dlg-section-head"><strong>代理线路</strong><span>支持 http / https / socks5 协议</span></div>
            <el-form-item label="名称"><el-input v-model="proxyForm.name" placeholder="例如：香港线路" maxlength="40" /></el-form-item>
            <el-form-item label="代理地址"><el-input v-model="proxyForm.address" class="mono-input" placeholder="socks5://127.0.0.1:1080" /><small class="form-tip">需要包含端口。直接粘贴 user:pass@host 会自动拆出账号密码。</small></el-form-item>
          </div>
          <div class="dlg-section">
            <div class="dlg-section-head"><strong>认证凭据（可选）</strong><span>无需身份验证时留空即可</span></div>
            <div class="proxy-auth-fields">
              <el-form-item label="用户名"><el-input v-model="proxyForm.username" placeholder="留空" autocomplete="off" /></el-form-item>
              <el-form-item label="密码">
                <el-input v-model="proxyForm.password" type="password" show-password autocomplete="new-password" :disabled="proxyForm.clearPassword" :placeholder="proxyForm.clearPassword ? '保存后清除' : proxyForm.hadPassword ? '已设置，留空不修改' : '留空'" />
              </el-form-item>
            </div>
            <p v-if="proxyForm.hadPassword" class="proxy-clear" style="margin-top: 8px">
              <template v-if="proxyForm.clearPassword">保存后会清除原密码。<el-button link type="primary" size="small" @click="proxyForm.clearPassword = false">撤销</el-button></template>
              <el-button v-else link type="danger" size="small" @click="proxyForm.clearPassword = true; proxyForm.password = ''">清除已保存的密码</el-button>
            </p>
          </div>
        </div>
      </el-form>
      <template #footer><el-button @click="proxyDialog = false">取消</el-button><el-button type="primary" :loading="proxySaving" :disabled="!proxyForm.name.trim() || !proxyForm.address.trim()" @click="saveProxy">保存代理</el-button></template>
    </el-dialog>

    <!-- 模型测试工作台弹窗 -->
    <el-dialog v-model="workbenchVisible" title="模型测试工作台" width="760px" class="model-workbench">
      <template v-if="testingModel">
        <div class="dlg-stack">
          <div class="dlg-section">
            <div class="workbench-model"><span class="entity-icon purple"><el-icon><MagicStick /></el-icon></span><div><strong>{{ testingModel.name }}</strong><span>{{ testingModel.model }} · {{ kindLabel(testingModel.kind) }} · {{ providerLabel(testingModel.provider, presets) }}</span></div></div>
            <el-segmented v-if="testingModel.kind === 'chat'" v-model="testMode" :options="testModeOptions" class="test-mode" @change="onTestModeChange" />
            <el-alert :title="`请求 endpoint：${testEndpoint}`" type="info" :closable="false" show-icon class="endpoint-alert" />
            <el-form label-position="top">
              <el-form-item v-if="testingModel.kind === 'chat' && testMode === 'chat'" label="System Prompt（可选）"><el-input v-model="testForm.systemPrompt" type="textarea" :rows="2" placeholder="例如：你是一名简洁、准确的客服助手" /></el-form-item>
              <el-form-item :label="inputLabel"><el-input v-model="testForm.input" type="textarea" :rows="testMode === 'web' ? 2 : 4" :placeholder="testingModel.kind === 'chat' ? '输入要发送给模型的消息' : testingModel.kind === 'decision' ? '输入一条群消息，看它判断要不要回复' : '输入需要生成向量的文本'" /></el-form-item>
              <el-button type="primary" :loading="testing" :disabled="!testForm.input.trim()" @click="runTest">{{ testMode === 'web' ? '运行联网测试' : '运行测试' }}</el-button>
            </el-form>
          </div>
          <el-alert v-if="testError" title="测试失败" type="error" :closable="false" show-icon class="test-error"><template #default><pre>{{ testError }}</pre><el-button size="small" :icon="CopyDocument" @click="copyText(testError)">复制错误</el-button></template></el-alert>
          <div v-if="testResult" class="dlg-section">
            <div class="result-heading"><h3>测试结果 · {{ testResult.model }}</h3><StatusBadge status="success" text="请求成功" /></div>
            <template v-if="testResult.kind === 'chat'">
              <div v-if="testMode === 'web'" class="search-trace" :class="{ hit: testResult.searched }">
                <div class="search-line"><el-icon><Search /></el-icon><strong>{{ testResult.searched ? `已联网${testResult.searchQueries?.length ? ` · 搜索 ${testResult.searchQueries.length} 次` : ''}` : '这次没有联网' }}</strong></div>
                <div v-if="testResult.searchQueries?.length" class="search-queries"><span v-for="q in testResult.searchQueries" :key="q" class="pill idle">{{ q }}</span></div>
                <ol v-if="testResult.sources?.length" class="search-sources"><li v-for="src in testResult.sources" :key="src.url"><a :href="src.url" target="_blank" rel="noopener noreferrer">{{ src.title || src.url }}</a><small>{{ src.siteName || hostOf(src.url) }}</small></li></ol>
              </div>
              <div class="result-output">{{ testResult.content }}</div>
              <div class="result-stats"><div><span>耗时</span><strong>{{ testResult.latencyMs }}ms</strong></div><div><span>输入 Token</span><strong>{{ testResult.inputTokens }}</strong></div><div><span>输出 Token</span><strong>{{ testResult.outputTokens }}</strong></div></div>
            </template>
            <template v-else-if="testResult.kind === 'decision'">
              <ul class="verdicts"><li v-for="v in verdicts(testResult.decision)" :key="v.label" :class="{ on: v.on }"><span class="verdict-mark"><el-icon><component :is="v.on ? Select : SemiSelect" /></el-icon></span><span class="verdict-label">{{ v.label }}</span><strong>{{ v.text }}</strong></li></ul>
              <details class="raw-json"><summary>原始返回</summary><pre>{{ prettyJSON(testResult.content) }}</pre></details>
              <div class="result-stats"><div><span>耗时</span><strong>{{ testResult.latencyMs }}ms</strong></div><div><span>输入 Token</span><strong>{{ testResult.inputTokens }}</strong></div><div><span>输出 Token</span><strong>{{ testResult.outputTokens }}</strong></div></div>
            </template>
            <template v-else-if="testResult.kind === 'image'"><el-image v-if="testResult.imageUrl" :src="testResult.imageUrl" :preview-src-list="[testResult.imageUrl]" fit="contain" class="image-result" /><div class="result-stats"><div><span>耗时</span><strong>{{ testResult.latencyMs }}ms</strong></div></div></template>
            <template v-else><div class="result-stats"><div><span>耗时</span><strong>{{ testResult.latencyMs }}ms</strong></div><div><span>向量维度</span><strong>{{ testResult.dimensions }}</strong></div><div><span>预览项数</span><strong>{{ Math.min(12, testResult.vectorPreview.length) }}</strong></div></div><pre class="vector-preview">[{{ testResult.vectorPreview.slice(0, 12).map(value => Number(value).toFixed(6)).join(', ') }}{{ testResult.vectorPreview.length > 12 ? ', …' : '' }}]</pre></template>
          </div>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ChatDotSquare, Coin, CopyDocument, Finished, MagicStick, Picture as PictureIcon, Plus, Search, Select, SemiSelect, VideoPlay } from '@element-plus/icons-vue'
import { modelProviderPresets, officialSearchModes, providerLabel } from '@/utils/model-providers'
import EmptyState from '@/components/EmptyState.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import ModelTopology3D from '@/components/three/ModelTopology3D.vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ApiError } from '@/api/client'
import { api, sanitizeServiceError } from '@/api'
import { animateStagger } from '@/utils/motion'
import type { ModelDecision, ModelKind, ModelProfile, ModelProviderId, ModelProviderPreset, ModelProxy, ModelTestResult, ProxyTestResult } from '@/types'

const pageRef = ref<HTMLElement>()
const route = useRoute()
const router = useRouter()
const tabOptions = [{ label: '模型配置', value: 'models' }, { label: '网络代理', value: 'proxies' }]
const activeTab = ref<'models' | 'proxies'>(route.query.tab === 'proxies' ? 'proxies' : 'models')
const showTopology = ref(true)

watch(() => route.query.tab, (val) => {
  if (val === 'proxies' || val === 'models') activeTab.value = val
})
function syncTabQuery(val: string | number) {
  router.replace({ query: { ...route.query, tab: String(val) } })
  nextTick(() => animateStagger(pageRef.value))
}
function goProxyTab() {
  dialog.value = false
  activeTab.value = 'proxies'
  syncTabQuery('proxies')
}
function onSelectProxyFrom3D(_p: ModelProxy) {
  activeTab.value = 'proxies'
  syncTabQuery('proxies')
}

// 模型状态与操作
const models = ref<ModelProfile[]>([])
const dialog = ref(false), dialogTab = ref<'basic' | 'advanced' | 'pricing'>('basic'), apiKey = ref(''), extraBodyText = ref(''), workbenchVisible = ref(false), testing = ref(false), testingModel = ref<ModelProfile>(), testResult = ref<ModelTestResult>(), testError = ref('')
const form = reactive<Partial<ModelProfile>>({})
type PriceKey = 'inputPricePerMillion' | 'outputPricePerMillion' | 'cacheReadPricePerMillion' | 'cacheWritePricePerMillion'
const priceFields: Array<{ key: PriceKey; label: string }> = [{ key: 'inputPricePerMillion', label: '输入' }, { key: 'outputPricePerMillion', label: '输出' }, { key: 'cacheReadPricePerMillion', label: '缓存读取' }, { key: 'cacheWritePricePerMillion', label: '缓存写入' }]
const shownPriceFields = computed(() => form.kind === 'decision' ? priceFields.slice(0, 1) : form.kind === 'embedding' ? priceFields.slice(0, 2) : priceFields)
function kindLabel(kind: ModelKind) { return ({ chat: '对话', embedding: 'Embedding', image: '生图', decision: '判断' } as Record<ModelKind, string>)[kind] }
function verdicts(d: ModelDecision) { return [{ label: '要不要回复', on: d.reply, text: d.reply ? '需要回复' : '不用回复' }, { label: '只是寒暄', on: d.socialOnly, text: d.socialOnly ? '是' : '不是' }, { label: '需要联网查', on: d.needsWebSearch, text: d.needsWebSearch ? '需要' : '不需要' }] }
function prettyJSON(text: string) { try { return JSON.stringify(JSON.parse(text), null, 2) } catch { return text } }
function hasPrice(m: ModelProfile) { return m.kind === 'image' ? m.pricePerImage != null : m.inputPricePerMillion != null || m.outputPricePerMillion != null }
function priceText(m: ModelProfile) { if (!hasPrice(m)) return '未设置'; if (m.kind === 'image') return `¥${m.pricePerImage} / 张`; if (m.kind === 'decision') return m.inputPricePerMillion == null ? '未设置' : `¥${m.inputPricePerMillion}`; const f = (v?: number | null) => v == null ? '—' : '¥' + v; return `${f(m.inputPricePerMillion)} / ${f(m.outputPricePerMillion)}` }
const groups = computed(() => [
  { kind: 'chat' as const, title: '对话模型', hint: '用来生成回复', empty: '至少需要一个对话模型，机器人才能回复消息。', items: models.value.filter(m => m.kind === 'chat') },
  { kind: 'decision' as const, title: '判断模型', hint: '只判断群消息要不要回复，比对话模型快、便宜', empty: '可选。添加 TypeSafe 后，到智能体工坊的“辅助模型”里选择。', items: models.value.filter(m => m.kind === 'decision') },
  { kind: 'embedding' as const, title: 'Embedding 模型', hint: '用来给知识库建索引、检索相关内容', empty: '知识库需要 Embedding 模型才能建索引。', items: models.value.filter(m => m.kind === 'embedding') },
  { kind: 'image' as const, title: '生图模型', hint: '“生成图片”技能使用，在每个 AI 客服里选择用哪一个', empty: '添加后，到 AI 客服的“生图模型”里选择。', items: models.value.filter(m => m.kind === 'image') },
])
const testForm = reactive({ input: '', systemPrompt: '' })
const testEndpoint = computed(() => {
  if (!testingModel.value) return ''
  const base = testingModel.value.baseUrl.replace(/\/$/, '')
  if (testingModel.value.kind === 'decision') return `${base}/systemone`
  if (testingModel.value.provider === 'deepseek' || testingModel.value.provider === 'zhipu') return `${base}/${testingModel.value.kind === 'embedding' ? 'embeddings' : testingModel.value.kind === 'image' ? 'images/generations' : 'chat/completions'}`
  if (testingModel.value.kind === 'embedding') return `${base}/embeddings`
  if (testingModel.value.kind === 'image') return `${base}/images/generations`
  return `${base}/${testingModel.value.webSearchMode === 'responses' ? 'responses' : 'chat/completions'}`
})
const proxies = ref<ModelProxy[]>([]), proxiesLoaded = ref(true), proxyDialog = ref(false), proxySaving = ref(false)
const proxyForm = reactive({ id: '', name: '', address: '', username: '', password: '', hadPassword: false, clearPassword: false })
const proxyTests = reactive<Record<string, Partial<ProxyTestResult> & { testing?: boolean }>>({})

const presets = ref<ModelProviderPreset[]>(modelProviderPresets)
const providerOptions = computed(() => [...presets.value.map(p => ({ label: p.name, value: p.id })), { label: '兼容接口', value: 'compatible' }])
const currentPreset = computed(() => presets.value.find(p => p.id === form.provider))
function presetModels(kind?: ModelKind) { return currentPreset.value?.models.filter(m => m.kind === kind) || [] }
const presetSupportsSearch = computed(() => { const m = presetModels('chat').find(x => x.model === form.model); return m ? m.supportsWebSearch !== false : true })
const officialSearch = computed({ get: () => officialSearchModes.includes(form.webSearchMode!), set: (on: boolean) => { form.webSearchMode = on && currentPreset.value ? currentPreset.value.webSearchMode : 'disabled' } })
function applyPreset() { const p = currentPreset.value; if (!p) return; form.baseUrl = p.baseUrl; const list = presetModels(form.kind); if (!list.some(m => m.model === form.model)) form.model = list[0]?.model || ''; onPresetModel() }
function onPresetModel() { const p = currentPreset.value; if (!p) return; const m = presetModels(form.kind).find(x => x.model === form.model); if (form.kind === 'embedding' && m?.dimension) form.dimension = m.dimension; if (form.kind === 'chat') form.webSearchMode = m?.supportsWebSearch === false ? 'disabled' : (officialSearchModes.includes(form.webSearchMode!) || !form.id ? p.webSearchMode : form.webSearchMode) }
const providerChoice = computed({
  get: () => (form.provider || 'compatible') as ModelProviderId,
  set: (v: ModelProviderId) => {
    const was = form.provider; form.provider = v
    if (v === 'compatible') { if (form.kind === 'decision') form.kind = 'chat'; if (was !== 'compatible') { form.baseUrl = ''; form.model = ''; if (officialSearchModes.includes(form.webSearchMode!)) form.webSearchMode = 'disabled' } }
    else { if (currentPreset.value && !presetModels(form.kind).length) form.kind = currentPreset.value.models[0]?.kind || 'chat'; applyPreset() }
  },
})
watch(() => form.kind, () => { if (currentPreset.value && dialog.value) applyPreset() })
const testMode = ref<'chat' | 'web'>('chat')
const testModeOptions = computed(() => [{ label: '普通对话', value: 'chat' }, { label: '联网测试', value: 'web', disabled: testingModel.value?.webSearchMode === 'disabled' }])
const webPrompt = '今天有哪些值得关注的科技新闻？请列出 3 条并注明来源。'
function onTestModeChange() { testResult.value = undefined; testError.value = ''; testForm.input = testMode.value === 'web' ? webPrompt : '请用一句话介绍你自己。' }
const inputLabel = computed(() => { const m = testingModel.value; if (!m) return ''; if (m.kind === 'image') return '画面描述'; if (m.kind === 'embedding') return '测试文本'; if (m.kind === 'decision') return '群消息'; return testMode.value === 'web' ? '需要实时信息的问题' : '用户消息' })
function hostOf(url: string) { try { return new URL(url).hostname } catch { return url } }
const discovered = ref<string[]>([]), discovering = ref(false), discoverError = ref('')
const extraDiscovered = computed(() => { const known = new Set(presetModels(form.kind).map(m => m.model)); return discovered.value.filter(id => !known.has(id)) })
const canDiscover = computed(() => (!!form.id || !!apiKey.value.trim()) && (!!currentPreset.value || !!form.baseUrl?.trim()))
const discoverTip = computed(() => discoverError.value || (discovered.value.length ? `已从接口读到 ${discovered.value.length} 个模型` : canDiscover.value ? '可从列表选或直接输入' : '填好参数后可读取可用模型'))
watch(() => [form.provider, form.baseUrl, form.proxyId], () => { discovered.value = []; discoverError.value = '' })
async function discover() {
  discovering.value = true; discoverError.value = ''
  try {
    const list = await api.models.discover({ provider: (form.provider || 'compatible') as ModelProviderId, baseUrl: currentPreset.value ? undefined : form.baseUrl?.trim(), apiKey: apiKey.value.trim() || undefined, profileId: form.id || undefined, proxyId: form.proxyId || '' })
    discovered.value = [...new Set(list)].sort()
    if (!list.length) discoverError.value = '接口没有返回模型，可以直接输入模型名。'
  } catch (error) { discovered.value = []; discoverError.value = sanitizeServiceError(error instanceof Error ? error.message : '') || '获取失败' }
  finally { discovering.value = false }
}
const proxyChoice = computed({ get: () => form.proxyId || 'direct', set: (v: string) => { form.proxyId = v === 'direct' ? null : v } })
function proxyName(id?: string | null) { return !id ? '直连' : proxies.value.find(p => p.id === id)?.name || '代理已失效' }

onMounted(async () => { await load(); nextTick(() => animateStagger(pageRef.value)) })
async function load() {
  const [list, proxyList, presetList] = await Promise.allSettled([api.models.list(), api.system.proxies.list(), api.models.presets()])
  if (presetList.status === 'fulfilled') presets.value = presetList.value
  if (list.status === 'fulfilled') models.value = list.value
  else ElMessage.error(list.reason instanceof Error ? list.reason.message : '模型读取失败')
  if (proxyList.status === 'fulfilled') { proxies.value = proxyList.value; proxiesLoaded.value = true }
  else proxiesLoaded.value = false
}
function searchModeText(mode: ModelProfile['webSearchMode']) { return ({ disabled: '关闭', responses: 'Responses Web Search', qwen: 'Qwen 内置搜索', openai: 'Chat Completions Web Search', custom: '自定义参数', deepseek: 'DeepSeek 官方联网', zhipu: '智谱官方联网' } as Record<string, string>)[mode] || '关闭' }
function reasoningText(level: ModelProfile['reasoningEffort']) { return ({ default: '模型默认', none: '关闭', minimal: '最小', low: '低', medium: '中', high: '高' } as Record<string, string>)[level] || '模型默认' }
function openCreate(kind: ModelKind = 'chat') {
  const decisionPreset = kind === 'decision' ? presets.value.find(p => p.models.some(m => m.kind === 'decision')) : undefined
  Object.assign(form, { id: '', name: '', kind, provider: decisionPreset?.id || 'compatible', model: kind === 'image' ? 'gpt-image-2.6-flare' : decisionPreset?.models.find(m => m.kind === 'decision')?.model || '', baseUrl: decisionPreset?.baseUrl || '', dimension: 1024, status: 'online', webSearchMode: 'disabled', reasoningEffort: 'default', extraBody: {}, inputPricePerMillion: null, outputPricePerMillion: null, cacheReadPricePerMillion: null, cacheWritePricePerMillion: null, pricePerImage: null, proxyId: null })
  apiKey.value = ''; extraBodyText.value = ''; discovered.value = []; discoverError.value = ''; dialogTab.value = 'basic'; dialog.value = true
}
function edit(m: ModelProfile) { Object.assign(form, m); apiKey.value = ''; extraBodyText.value = Object.keys(m.extraBody || {}).length ? JSON.stringify(m.extraBody, null, 2) : ''; discovered.value = []; discoverError.value = ''; dialogTab.value = 'basic'; dialog.value = true }
async function save() {
  try { form.extraBody = extraBodyText.value.trim() ? JSON.parse(extraBodyText.value) : {} }
  catch { ElMessage.error('额外请求参数必须是有效的 JSON 对象'); return }
  if (Array.isArray(form.extraBody) || form.extraBody === null || typeof form.extraBody !== 'object') { ElMessage.error('额外请求参数必须是 JSON 对象'); return }
  await api.models.save(form, apiKey.value)
  await load()
  dialog.value = false
  ElMessage.success('模型配置已保存')
}
function openWorkbench(model: ModelProfile) {
  testingModel.value = model
  testForm.input = model.kind === 'chat' ? '请用一句话介绍你自己。' : model.kind === 'decision' ? '小Q在吗？我的订单一直显示处理中，帮忙看下' : model.kind === 'image' ? '一只坐在窗台上的橘猫，午后阳光' : '这是一段用于测试向量模型的文本。'
  testForm.systemPrompt = ''; testMode.value = 'chat'; testResult.value = undefined; testError.value = ''; workbenchVisible.value = true
}
async function runTest() {
  if (!testingModel.value || !testForm.input.trim()) return
  testing.value = true; testResult.value = undefined; testError.value = ''
  try {
    const result = await api.models.test(testingModel.value.id, { input: testForm.input.trim(), systemPrompt: testingModel.value.kind === 'chat' && testMode.value === 'chat' && testForm.systemPrompt.trim() ? testForm.systemPrompt.trim() : undefined, webSearch: testingModel.value.kind === 'chat' && testMode.value === 'web' ? true : undefined })
    testResult.value = result; testingModel.value.latency = result.latencyMs; testingModel.value.status = 'online'
  } catch (error) {
    testingModel.value.status = 'warning'
    testError.value = sanitizeServiceError(error instanceof Error ? error.message : '模型测试失败') || '模型测试失败'
  } finally { testing.value = false }
}
async function copyText(value: string) { await navigator.clipboard?.writeText(value); ElMessage.success('错误信息已复制') }

// 代理 CRUD 与测速（已从原 SystemView 完整迁移至此）
function openProxy(p?: ModelProxy) {
  Object.assign(proxyForm, { id: p?.id || '', name: p?.name || '', address: p?.address || '', username: p?.username || '', password: '', hadPassword: !!p?.hasPassword, clearPassword: false })
  proxyDialog.value = true
}
async function saveProxy() {
  proxySaving.value = true
  try {
    await api.system.proxies.save({ id: proxyForm.id || undefined, name: proxyForm.name.trim(), address: proxyForm.address.trim(), username: proxyForm.username.trim(), password: proxyForm.clearPassword ? '' : proxyForm.password, clearPassword: proxyForm.clearPassword })
    if (proxyForm.id) delete proxyTests[proxyForm.id]
    proxyDialog.value = false
    await load()
    ElMessage.success('代理已保存')
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '保存代理失败') }
  finally { proxySaving.value = false }
}
async function testProxy(p: ModelProxy) {
  proxyTests[p.id] = { testing: true }
  try {
    const result = await api.system.proxies.test(p.id)
    proxyTests[p.id] = result
    if (!result.reachable) ElMessage.error(`${p.name} 连不通：${result.error || '没有返回原因'}`)
  } catch (error) {
    proxyTests[p.id] = { reachable: false, error: error instanceof Error ? error.message : '测试失败' }
    ElMessage.error(proxyTests[p.id].error!)
  }
}
function proxyBadge(id: string) {
  const t = proxyTests[id]
  if (t.testing) return { status: 'processing', text: '测试中' }
  return t.reachable ? { status: 'success', text: `连通 · ${t.latencyMs}ms` } : { status: 'failed', text: '连不通' }
}
async function removeProxy(p: ModelProxy) {
  try { await ElMessageBox.confirm(`删除后不能恢复。确定删除代理“${p.name}”吗？`, '删除代理', { confirmButtonText: '删除代理', cancelButtonText: '取消', confirmButtonClass: 'el-button--danger' }) } catch { return }
  try { await api.system.proxies.remove(p.id); delete proxyTests[p.id]; await load(); ElMessage.success('代理已删除') }
  catch (error) { ElMessage.error(error instanceof ApiError && error.code === 'proxy_in_use' ? `还有模型在使用“${p.name}”，先在本页“模型配置”里把它们改成直连或其他代理。` : error instanceof Error ? error.message : '删除失败') }
}

useAdminEvents(['model_profiles', 'model_proxies'], load)
</script>

<style scoped>
.topo-stage { position: relative; margin-bottom: 20px; }
.topo-hud {
  position: absolute;
  left: 14px;
  top: 12px;
  z-index: 4;
  display: flex;
  flex-direction: column;
  gap: 8px;
  pointer-events: none;
}
.topo-tip { color: var(--muted); font-size: 11px; }
.proxy-panel .panel-body { padding-top: 8px; }
.proxy-list { display: flex; flex-direction: column; }
.proxy-row { display: grid; grid-template-columns: minmax(0, 1fr) 118px auto; align-items: center; gap: 12px; padding: 12px 0; border-top: 1px solid var(--line); }
.proxy-row:first-child { border-top: 0; padding-top: 0; }
.proxy-name { display: flex; flex-direction: column; min-width: 0; }
.proxy-name strong { font-weight: 600; }
.proxy-meta { display: flex; gap: 8px; min-width: 0; color: var(--muted); font-size: 12px; white-space: nowrap; }
.proxy-address { overflow: hidden; text-overflow: ellipsis; }
.proxy-actions { display: flex; align-items: center; gap: 4px; }
.proxy-auth-fields { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.proxy-clear { margin: -8px 0 0; color: var(--muted); font-size: 12px; }

.price-item :deep(.el-form-item__content) { display: block; }
.proxy-pill { flex: none; }
.option-sub { margin-left: 8px; color: var(--muted); font-size: 12px; }
.price-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; width: 100%; }
.price-grid label { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.price-grid span { color: var(--muted); font-size: 12px; }
.price-grid .el-input-number { width: 100%; }
.price-grid.two { grid-template-columns: repeat(2, minmax(0, 1fr)); max-width: 50%; }
.price-single { width: 200px; }
.price-grid.one { grid-template-columns: 1fr; max-width: 25%; }
.verdicts { display: grid; gap: 6px; margin: 0 0 10px; padding: 0; list-style: none; }
.verdicts li { display: flex; align-items: center; gap: 10px; padding: 8px 12px; border: 1px solid var(--line); border-radius: var(--radius-sm); background: var(--surface-2); }
.verdicts li.on { border-color: var(--primary-light); background: var(--primary-light); }
.verdict-mark { display: inline-flex; width: 20px; height: 20px; align-items: center; justify-content: center; border-radius: 50%; background: var(--surface-3); color: var(--muted); font-size: 12px; }
.verdicts li.on .verdict-mark { background: var(--primary); color: #fff; }
.verdict-label { flex: 1; color: var(--text-2); font-size: 13px; }
.verdicts li.on strong { color: var(--primary-text); }
.raw-json { margin-bottom: 10px; font-size: 13px; }
.raw-json pre { margin: 6px 0 0; padding: 10px; max-height: 200px; overflow: auto; border-radius: var(--radius-sm); background: var(--surface-2); font-size: 12px; white-space: pre-wrap; }
.entity-facts strong.unset { color: var(--muted); font-weight: 400; }
.provider-switch { width: 100%; }
.model-pick { display: flex; gap: 8px; width: 100%; }
.model-pick .el-select { flex: 1; min-width: 0; }
.switch-line { display: flex; align-items: center; gap: 10px; color: var(--muted); font-size: 13px; }
.test-mode { margin-bottom: 12px; }
.search-trace { margin-bottom: 12px; padding: 10px 12px; border: 1px solid var(--line); border-radius: var(--radius-sm); background: var(--surface-2); }
.search-line { display: flex; flex-wrap: wrap; align-items: center; gap: 6px 8px; }
.search-queries { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
.search-sources { margin: 8px 0 0; padding-left: 20px; max-height: 160px; overflow: auto; font-size: 13px; }
.search-sources a { color: var(--primary-text); }
.search-sources small { margin-left: 8px; color: var(--muted); font-size: 12px; }
.image-result { display: block; width: 100%; max-height: 360px; margin-bottom: 12px; border-radius: 8px; background: var(--surface-2); }
@media (max-width: 640px) { .price-grid { grid-template-columns: 1fr 1fr; } .proxy-auth-fields { grid-template-columns: 1fr; } }
</style>
