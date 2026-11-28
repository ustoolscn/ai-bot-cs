<template>
  <div ref="hostRef" class="topo-canvas" @pointermove="onPointerMove" @pointerleave="onPointerLeave" @click="onClick">
    <div class="topo-labels" aria-hidden="true">
      <div
        v-for="tag in labels"
        :key="tag.key"
        class="topo-tag"
        :class="[tag.tone, { clickable: tag.clickable }]"
        :style="{ transform: `translate3d(${tag.x}px, ${tag.y}px, 0) translate(-50%, -120%)` }"
        @click.stop="onTagClick(tag.action)"
      >
        <i /><span class="topo-name">{{ tag.text }}</span><code v-if="tag.sub" class="topo-sub">{{ tag.sub }}</code>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as THREE from 'three'
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js'
import { EffectComposer } from 'three/examples/jsm/postprocessing/EffectComposer.js'
import { RenderPass } from 'three/examples/jsm/postprocessing/RenderPass.js'
import { UnrealBloomPass } from 'three/examples/jsm/postprocessing/UnrealBloomPass.js'
import { OutputPass } from 'three/examples/jsm/postprocessing/OutputPass.js'
import type { ModelProfile, ModelProxy, ProxyTestResult } from '@/types'

const props = defineProps<{
  models: ModelProfile[]
  proxies: ModelProxy[]
  proxyTests?: Record<string, Partial<ProxyTestResult> & { testing?: boolean }>
}>()

const emit = defineEmits<{
  (e: 'selectModel', model: ModelProfile): void
  (e: 'selectProxy', proxy: ModelProxy): void
}>()

type NodeAction = { type: 'model'; item: ModelProfile } | { type: 'proxy'; item: ModelProxy }

interface ScreenTag {
  key: string
  text: string
  sub?: string
  tone: string
  clickable: boolean
  action?: NodeAction
  x: number
  y: number
}

const kindColor: Record<string, number> = {
  chat: 0x38bdf8,
  decision: 0xfbbf24,
  embedding: 0x34d399,
  image: 0xa855f7,
}

const hostRef = ref<HTMLElement>()
const labels = ref<ScreenTag[]>([])

let renderer: THREE.WebGLRenderer | null = null
let composer: EffectComposer | null = null
let bloomPass: UnrealBloomPass | null = null
let scene: THREE.Scene | null = null
let camera: THREE.PerspectiveCamera | null = null
let controls: OrbitControls | null = null
let resizeObserver: ResizeObserver | null = null
let rafId = 0

let routerCore: THREE.Mesh | null = null
let dynamicGroup: THREE.Group | null = null

const raycaster = new THREE.Raycaster()
const pointer = new THREE.Vector2(-9, -9)
const pickables: THREE.Object3D[] = []
const nodeAction = new Map<THREE.Object3D, NodeAction>()
const projectedAnchors: Array<{ key: string; text: string; sub?: string; tone: string; clickable: boolean; action?: NodeAction; pos: THREE.Vector3 }> = []
const pulses: Array<{ curve: THREE.CubicBezierCurve3; mesh: THREE.Mesh; speed: number; offset: number }> = []
const staticDisposables: Array<{ dispose: () => void }> = []
const dynamicDisposables: Array<{ dispose: () => void }> = []

function trackStatic<T extends { dispose: () => void }>(item: T): T {
  staticDisposables.push(item)
  return item
}
function trackDynamic<T extends { dispose: () => void }>(item: T): T {
  dynamicDisposables.push(item)
  return item
}

function clearDynamic() {
  if (dynamicGroup) {
    while (dynamicGroup.children.length > 0) dynamicGroup.remove(dynamicGroup.children[0])
  }
  for (const item of dynamicDisposables) item.dispose()
  dynamicDisposables.length = 0
  pickables.length = 0
  nodeAction.clear()
  projectedAnchors.length = 0
  pulses.length = 0
}

function addCurveWithPulse(from: THREE.Vector3, to: THREE.Vector3, colorHex: number, speed: number) {
  if (!dynamicGroup) return
  const midX = (from.x + to.x) * 0.5
  const curve = new THREE.CubicBezierCurve3(
    from,
    new THREE.Vector3(midX, from.y + 0.9, from.z * 0.6),
    new THREE.Vector3(midX, to.y + 0.9, to.z * 0.6),
    to,
  )
  const pts = curve.getPoints(28)
  const geo = trackDynamic(new THREE.BufferGeometry().setFromPoints(pts))
  const mat = trackDynamic(new THREE.LineBasicMaterial({
    color: colorHex,
    transparent: true,
    opacity: 0.34,
    blending: THREE.AdditiveBlending,
    depthWrite: false,
  }))
  dynamicGroup.add(new THREE.Line(geo, mat))

  const dotGeo = trackDynamic(new THREE.SphereGeometry(0.085, 8, 8))
  const dotMat = trackDynamic(new THREE.MeshBasicMaterial({ color: colorHex }))
  const dot = new THREE.Mesh(dotGeo, dotMat)
  dynamicGroup.add(dot)
  pulses.push({ curve, mesh: dot, speed, offset: Math.random() })
}

function rebuildTopology() {
  if (!dynamicGroup) return
  clearDynamic()

  const corePos = new THREE.Vector3(-6.2, 0.2, 0)
  projectedAnchors.push({ key: 'core', text: 'AI 路由网关', tone: 'cyan', clickable: false, pos: new THREE.Vector3(-6.2, 1.5, 0) })

  // 中转层：直连节点 + 每个代理水晶
  const proxyPosMap = new Map<string, THREE.Vector3>()
  const directPos = new THREE.Vector3(-1.2, 1.8, -1.6)
  const directGeo = trackDynamic(new THREE.OctahedronGeometry(0.32, 0))
  const directMat = trackDynamic(new THREE.MeshBasicMaterial({ color: 0x64748b, wireframe: true, transparent: true, opacity: 0.7 }))
  const directMesh = new THREE.Mesh(directGeo, directMat)
  directMesh.position.copy(directPos)
  dynamicGroup.add(directMesh)
  projectedAnchors.push({ key: 'direct', text: '直连通道', tone: 'muted', clickable: false, pos: new THREE.Vector3(directPos.x, directPos.y + 0.55, directPos.z) })
  addCurveWithPulse(corePos, directPos, 0x38bdf8, 0.48)

  const proxies = props.proxies || []
  proxies.forEach((p, idx) => {
    const py = -1.6 + (idx - (proxies.length - 1) * 0.5) * 1.45
    const pz = 1.4 + (idx % 2) * 1.1
    const pos = new THREE.Vector3(-1.2, py, pz)
    proxyPosMap.set(p.id, pos)

    const test = props.proxyTests?.[p.id]
    const color = test?.testing ? 0xfbbf24 : test?.reachable === true ? 0x34d399 : test?.reachable === false ? 0xf87171 : 0x38bdf8
    const geo = trackDynamic(new THREE.OctahedronGeometry(0.46, 0))
    const mat = trackDynamic(new THREE.MeshBasicMaterial({ color, transparent: true, opacity: 0.9 }))
    const crystal = new THREE.Mesh(geo, mat)
    crystal.position.copy(pos)
    dynamicGroup!.add(crystal)

    const act: NodeAction = { type: 'proxy', item: p }
    pickables.push(crystal)
    nodeAction.set(crystal, act)
    projectedAnchors.push({
      key: `p-${p.id}`,
      text: `代理 · ${p.name}`,
      tone: test?.reachable === false ? 'danger' : test?.reachable ? 'green' : 'cyan',
      clickable: true,
      action: act,
      pos: new THREE.Vector3(pos.x, pos.y + 0.65, pos.z),
    })
    addCurveWithPulse(corePos, pos, color, 0.52)
  })

  // 模型集群
  const models = props.models || []
  models.forEach((m, idx) => {
    const total = Math.max(models.length, 1)
    const span = Math.min(7.4, total * 1.35)
    const y = ((idx / Math.max(total - 1, 1)) - 0.5) * span
    const z = Math.sin(idx * 0.95) * 2.4
    const pos = new THREE.Vector3(5.6, y, z)
    const isOffline = m.status === 'offline' || m.status === 'warning'
    const color = isOffline ? 0xf87171 : kindColor[m.kind] || 0x38bdf8

    const geo = trackDynamic(new THREE.SphereGeometry(0.42, 18, 18))
    const mat = trackDynamic(new THREE.MeshBasicMaterial({ color, transparent: true, opacity: 0.92 }))
    const orb = new THREE.Mesh(geo, mat)
    orb.position.copy(pos)
    dynamicGroup!.add(orb)

    const haloGeo = trackDynamic(new THREE.SphereGeometry(0.62, 12, 12))
    const haloMat = trackDynamic(new THREE.MeshBasicMaterial({
      color,
      transparent: true,
      opacity: 0.18,
      blending: THREE.AdditiveBlending,
      depthWrite: false,
    }))
    orb.add(new THREE.Mesh(haloGeo, haloMat))

    const act: NodeAction = { type: 'model', item: m }
    pickables.push(orb)
    nodeAction.set(orb, act)
    const cleanName = m.name.trim()
    const cleanModel = m.model.trim()
    const duplicate = !cleanName || !cleanModel || cleanName === cleanModel || cleanName.includes(cleanModel)
    projectedAnchors.push({
      key: `m-${m.id}`,
      text: cleanName || cleanModel || '未命名模型',
      sub: duplicate ? undefined : cleanModel,
      tone: isOffline ? 'danger' : m.kind === 'embedding' ? 'green' : m.kind === 'image' ? 'purple' : m.kind === 'decision' ? 'warn' : 'cyan',
      clickable: true,
      action: act,
      pos: new THREE.Vector3(pos.x, pos.y + 0.62, pos.z),
    })

    const via = m.proxyId && proxyPosMap.has(m.proxyId) ? proxyPosMap.get(m.proxyId)! : directPos
    const speed = m.latency ? Math.max(0.28, Math.min(1.1, 450 / m.latency)) : 0.45
    addCurveWithPulse(via, pos, color, speed)
  })
}

function updateLabels() {
  if (!camera || !hostRef.value) return
  const w = hostRef.value.clientWidth
  const h = hostRef.value.clientHeight
  if (!w || !h) return
  const v = new THREE.Vector3()
  const next: ScreenTag[] = []
  for (const a of projectedAnchors) {
    v.copy(a.pos).project(camera)
    if (v.z > 1 || v.z < -1) continue
    next.push({
      key: a.key,
      text: a.text,
      sub: a.sub,
      tone: a.tone,
      clickable: a.clickable,
      action: a.action,
      x: Math.round((v.x * 0.5 + 0.5) * w),
      y: Math.round((-v.y * 0.5 + 0.5) * h),
    })
  }
  labels.value = next
}

function onPointerMove(e: PointerEvent) {
  const host = hostRef.value
  if (!host || !camera) return
  const rect = host.getBoundingClientRect()
  pointer.x = ((e.clientX - rect.left) / Math.max(rect.width, 1)) * 2 - 1
  pointer.y = -((e.clientY - rect.top) / Math.max(rect.height, 1)) * 2 + 1
  raycaster.setFromCamera(pointer, camera)
  const hits = raycaster.intersectObjects(pickables, false)
  host.style.cursor = hits.length ? 'pointer' : 'grab'
}

function onPointerLeave() {
  pointer.set(-9, -9)
}

function onTagClick(act?: NodeAction) {
  if (!act) return
  if (act.type === 'model') emit('selectModel', act.item)
  else emit('selectProxy', act.item)
}

function onClick() {
  if (!camera || !pickables.length) return
  raycaster.setFromCamera(pointer, camera)
  const hits = raycaster.intersectObjects(pickables, false)
  if (!hits.length) return
  onTagClick(nodeAction.get(hits[0].object))
}

watch(() => [props.models, props.proxies, props.proxyTests], rebuildTopology, { deep: true })

onMounted(() => {
  const host = hostRef.value
  if (!host || typeof window === 'undefined') return
  const w = Math.max(host.clientWidth, 320)
  const h = Math.max(host.clientHeight, 240)

  try {
    renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true, powerPreference: 'high-performance' })
  } catch {
    return
  }
  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2))
  renderer.setSize(w, h)
  renderer.toneMapping = THREE.ACESFilmicToneMapping
  host.prepend(renderer.domElement)

  scene = new THREE.Scene()
  camera = new THREE.PerspectiveCamera(42, w / h, 0.1, 120)
  camera.position.set(0, 3.8, 14.5)

  controls = new OrbitControls(camera, renderer.domElement)
  controls.enableDamping = true
  controls.enableZoom = false
  controls.minPolarAngle = Math.PI * 0.25
  controls.maxPolarAngle = Math.PI * 0.62

  composer = new EffectComposer(renderer)
  composer.addPass(new RenderPass(scene, camera))
  bloomPass = new UnrealBloomPass(new THREE.Vector2(w, h), 0.7, 0.45, 0.25)
  composer.addPass(bloomPass)
  composer.addPass(new OutputPass())

  const grid = trackStatic(new THREE.GridHelper(26, 26, 0x1e3a5f, 0x0f1e36))
  grid.position.y = -4.2
  scene.add(grid)

  const coreGeo = trackStatic(new THREE.IcosahedronGeometry(0.92, 1))
  const coreMat = trackStatic(new THREE.MeshBasicMaterial({ color: 0x38bdf8, wireframe: true, transparent: true, opacity: 0.78 }))
  routerCore = new THREE.Mesh(coreGeo, coreMat)
  routerCore.position.set(-6.2, 0.2, 0)
  scene.add(routerCore)

  const ringGeo = trackStatic(new THREE.TorusGeometry(1.32, 0.025, 8, 48))
  const ringMat = trackStatic(new THREE.MeshBasicMaterial({ color: 0x818cf8, transparent: true, opacity: 0.6, blending: THREE.AdditiveBlending }))
  const coreRing = new THREE.Mesh(ringGeo, ringMat)
  coreRing.rotation.x = 1.15
  routerCore.add(coreRing)

  dynamicGroup = new THREE.Group()
  scene.add(dynamicGroup)
  rebuildTopology()

  if (typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(entries => {
      const rect = entries[0]?.contentRect
      if (!rect || rect.width <= 0 || rect.height <= 0 || !renderer || !composer || !camera) return
      const nw = Math.floor(rect.width)
      const nh = Math.floor(rect.height)
      camera.aspect = nw / nh
      camera.updateProjectionMatrix()
      renderer.setSize(nw, nh)
      composer.setSize(nw, nh)
      bloomPass?.setSize(nw, nh)
    })
    resizeObserver.observe(host)
  }

  const clock = new THREE.Clock()
  let tick = 0
  const animate = () => {
    rafId = window.requestAnimationFrame(animate)
    const dt = clock.getDelta()
    const t = clock.elapsedTime
    if (routerCore) {
      routerCore.rotation.y += dt * 0.45
      routerCore.rotation.x += dt * 0.16
    }
    for (const p of pulses) {
      const progress = (t * p.speed + p.offset) % 1
      p.curve.getPointAt(progress, p.mesh.position)
    }
    controls?.update()
    composer?.render()
    tick++
    if (tick % 2 === 0) updateLabels()
  }
  animate()
})

onBeforeUnmount(() => {
  if (rafId) window.cancelAnimationFrame(rafId)
  resizeObserver?.disconnect()
  controls?.dispose()
  clearDynamic()
  for (const d of staticDisposables) d.dispose()
  staticDisposables.length = 0
  composer?.dispose()
  if (renderer) {
    renderer.dispose()
    renderer.domElement.remove()
  }
  renderer = null
  composer = null
  scene = null
  camera = null
  controls = null
})
</script>

<style scoped>
.topo-canvas {
  position: relative;
  width: 100%;
  height: 290px;
  border: 1px solid rgba(56, 189, 248, 0.22);
  border-radius: var(--radius);
  overflow: hidden;
  background: radial-gradient(circle at 50% 45%, #0a162e 0%, #050914 75%);
  cursor: grab;
}
.topo-canvas :deep(canvas) {
  display: block;
  width: 100% !important;
  height: 100% !important;
}
.topo-labels {
  position: absolute;
  inset: 0;
  pointer-events: none;
  overflow: hidden;
}
.topo-tag {
  position: absolute;
  left: 0;
  top: 0;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  max-width: 240px;
  padding: 2px 8px;
  border: 1px solid rgba(148, 163, 184, 0.25);
  border-radius: 999px;
  background: rgba(7, 13, 27, 0.88);
  color: #e2e8f0;
  font-size: 11px;
  white-space: nowrap;
  backdrop-filter: blur(8px);
  transition: border-color 140ms, background 140ms, box-shadow 140ms;
}
.topo-tag.clickable {
  pointer-events: auto;
  cursor: pointer;
}
.topo-tag.clickable:hover {
  border-color: #38bdf8;
  background: rgba(14, 165, 233, 0.24);
  box-shadow: 0 0 12px rgba(56, 189, 248, 0.36);
}
.topo-name {
  overflow: hidden;
  text-overflow: ellipsis;
}
.topo-sub {
  padding-left: 5px;
  border-left: 1px solid rgba(148, 163, 184, 0.28);
  color: #7dd3fc;
  font-family: var(--font-mono);
  font-size: 10px;
  overflow: hidden;
  text-overflow: ellipsis;
}
.topo-tag i {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #38bdf8;
}
.topo-tag.green i { background: #34d399; }
.topo-tag.warn i { background: #fbbf24; }
.topo-tag.purple i { background: #a855f7; }
.topo-tag.danger i { background: #f87171; }
.topo-tag.muted i { background: #64748b; }
</style>
