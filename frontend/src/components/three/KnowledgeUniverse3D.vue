<template>
  <div ref="containerRef" class="universe-3d" @pointerdown="onPointerDown" @pointermove="onPointerMove" @pointerup="onPointerUp" @mouseleave="hoveredMeta = null">
    <div ref="canvasHostRef" class="webgl-host" />

    <!-- 3D -> 2D 全息浮标层 -->
    <div class="hud-labels" aria-hidden="true">
      <div
        v-for="lbl in hudLabels"
        :key="lbl.key"
        class="hud-tag"
        :class="[`kind-${lbl.kind}`, { 'is-hit': lbl.isHit, 'is-selected': lbl.isSelected }]"
        :style="{ transform: `translate3d(${lbl.x}px, ${lbl.y}px, 0) translate(-50%, -135%)` }"
      >
        <i class="dot" />
        <span class="txt">{{ lbl.text }}</span>
        <em v-if="lbl.score" class="score">{{ Math.round(lbl.score * 100) }}%</em>
      </div>
    </div>

    <!-- 悬停节点精简浮卡 -->
    <div v-if="hoveredMeta" class="hover-card" :style="{ left: hoverScreen.x + 'px', top: hoverScreen.y + 'px' }">
      <div class="hover-head">
        <span class="hover-kind">{{ kindText(hoveredMeta.kind) }}</span>
        <strong>{{ hoveredMeta.title }}</strong>
      </div>
      <small>{{ hoveredMeta.sub }}</small>
      <p v-if="hoveredMeta.snippet">{{ hoveredMeta.snippet }}</p>
    </div>

    <!-- 3D 宇宙为空时提示 -->
    <div v-if="isEmptyUniverse" class="universe-empty">
      <div class="empty-orb" />
      <h3>这座知识星系正在等待第一颗恒星</h3>
      <p>点击下方操作栏上传文档或创建问答，知识库会自动建立 3D 向量星图与拓扑链路。</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import * as THREE from 'three'
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js'
import { EffectComposer } from 'three/examples/jsm/postprocessing/EffectComposer.js'
import { RenderPass } from 'three/examples/jsm/postprocessing/RenderPass.js'
import { UnrealBloomPass } from 'three/examples/jsm/postprocessing/UnrealBloomPass.js'
import { OutputPass } from 'three/examples/jsm/postprocessing/OutputPass.js'
import gsap from 'gsap'
import type { KnowledgeInspectorSelection, KnowledgeMapData, KnowledgeSearchHit } from '@/types'

const props = withDefaults(defineProps<{
  mapData: KnowledgeMapData
  mode: 'galaxy' | 'semantic' | 'city'
  colorMode?: 'structure' | 'heat'
  selection: KnowledgeInspectorSelection
  searchQuery: string
  searchHits: KnowledgeSearchHit[]
  clusterFilter: number | null
  autoRotate: boolean
  unreadFeedbackCount: number
}>(), {
  colorMode: 'structure',
})

const emit = defineEmits<{
  select: [selection: KnowledgeInspectorSelection]
  navigateDrawer: [tab: 'documents' | 'entries' | 'candidates' | 'gaps' | 'feedback']
}>()

interface InteractiveMeta {
  key: string
  kind: 'hub' | 'document' | 'entry' | 'candidate' | 'gap' | 'agent' | 'conversation' | 'point' | 'cluster'
  title: string
  sub: string
  snippet?: string
  colorHex: number
  baseScale: number
  worldPos: THREE.Vector3
  selection?: KnowledgeInspectorSelection
  tabTarget?: 'documents' | 'entries' | 'candidates' | 'gaps' | 'feedback'
  sourceDocId?: string
  sourceEntryId?: string
  pointId?: string
  clusterId?: number
  priority: number
  mesh: THREE.Object3D
  material?: THREE.MeshStandardMaterial | THREE.MeshBasicMaterial
}

interface HudLabel {
  key: string
  kind: InteractiveMeta['kind']
  text: string
  x: number
  y: number
  score?: number
  isHit: boolean
  isSelected: boolean
}

interface CurveTraveler {
  curve: THREE.QuadraticBezierCurve3
  mesh: THREE.Mesh
  speed: number
  offset: number
}

const containerRef = ref<HTMLElement>()
const canvasHostRef = ref<HTMLElement>()
const hudLabels = ref<HudLabel[]>([])
const hoveredMeta = ref<InteractiveMeta | null>(null)
const hoverScreen = reactive({ x: 24, y: 24 })

const isEmptyUniverse = computed(() =>
  props.mapData.documents.length === 0
  && props.mapData.entries.length === 0
  && props.mapData.points.length === 0,
)

// WebGL 核心引用
let renderer: THREE.WebGLRenderer | null = null
let composer: EffectComposer | null = null
let bloomPass: UnrealBloomPass | null = null
let scene: THREE.Scene | null = null
let camera: THREE.PerspectiveCamera | null = null
let controls: OrbitControls | null = null
let resizeObserver: ResizeObserver | null = null
let rafId = 0

// 场景分组
let rootGroup: THREE.Group | null = null
let modeGroup: THREE.Group | null = null
let probeGroup: THREE.Group | null = null
let floorGrid: THREE.GridHelper | null = null
let starField: THREE.Points | null = null
let hubRings: Array<{ mesh: THREE.Object3D; sx: number; sy: number; sz: number }> = []
let probeWaves: THREE.Mesh[] = []
let travelers: CurveTraveler[] = []

const globalDisposables: Array<{ dispose: () => void }> = []
const modeDisposables: Array<{ dispose: () => void }> = []
const probeDisposables: Array<{ dispose: () => void }> = []
const interactiveMetas: InteractiveMeta[] = []
const interactiveMeshes: THREE.Object3D[] = []

const raycaster = new THREE.Raycaster()
const pointerNDC = new THREE.Vector2()
const projVec = new THREE.Vector3()
let dragOrigin = { x: 0, y: 0, moved: false }
let activeTween: gsap.core.Tween | null = null
let frameCounter = 0

const clusterPalette = [0x38bdf8, 0x34d399, 0x818cf8, 0xfbbf24, 0xf87171, 0x2dd4bf]

function trackGlobal<T extends { dispose: () => void }>(item: T): T {
  globalDisposables.push(item)
  return item
}
function trackMode<T extends { dispose: () => void }>(item: T): T {
  modeDisposables.push(item)
  return item
}
function trackProbe<T extends { dispose: () => void }>(item: T): T {
  probeDisposables.push(item)
  return item
}

function disposeGroup(group: THREE.Group | null, list: Array<{ dispose: () => void }>) {
  if (group) {
    while (group.children.length > 0) group.remove(group.children[0])
  }
  for (const d of list) d.dispose()
  list.length = 0
}

// 计算命中的映射分数
const hitScoresById = computed(() => {
  const byChunk = new Map<string, number>()
  const byDoc = new Map<string, number>()
  const byEntry = new Map<string, number>()
  for (const h of props.searchHits) {
    if (h.id) byChunk.set(h.id, Math.max(byChunk.get(h.id) || 0, h.score))
    if (h.documentId) byDoc.set(h.documentId, Math.max(byDoc.get(h.documentId) || 0, h.score))
    if (h.entryId) byEntry.set(h.entryId, Math.max(byEntry.get(h.entryId) || 0, h.score))
  }
  return { byChunk, byDoc, byEntry }
})

function hitScoreForMeta(meta: InteractiveMeta): number | undefined {
  if (!props.searchHits.length) return undefined
  if (meta.pointId && hitScoresById.value.byChunk.has(meta.pointId)) return hitScoresById.value.byChunk.get(meta.pointId)
  if (meta.sourceDocId && hitScoresById.value.byDoc.has(meta.sourceDocId)) return hitScoresById.value.byDoc.get(meta.sourceDocId)
  if (meta.sourceEntryId && hitScoresById.value.byEntry.has(meta.sourceEntryId)) return hitScoresById.value.byEntry.get(meta.sourceEntryId)
  return undefined
}

function isMetaSelected(meta: InteractiveMeta): boolean {
  const sel = props.selection
  if (!sel || !meta.selection) return false
  if (sel.type !== meta.selection.type) return false
  if (sel.type === 'point' && meta.selection.type === 'point') return sel.point.id === meta.selection.point.id
  return 'id' in sel && 'id' in meta.selection && sel.id === meta.selection.id
}

function flyCameraTo(target: THREE.Vector3, offset: THREE.Vector3, duration = 0.95) {
  if (!camera || !controls) return
  activeTween?.kill()
  const destCam = target.clone().add(offset)
  const ctrlTarget = controls.target
  const tl = gsap.timeline()
  tl.to(camera.position, {
    x: destCam.x, y: destCam.y, z: destCam.z,
    duration, ease: 'power3.out',
    onUpdate: () => controls?.update(),
  }, 0)
  tl.to(ctrlTarget, {
    x: target.x, y: target.y, z: target.z,
    duration, ease: 'power3.out',
    onUpdate: () => controls?.update(),
  }, 0)
  activeTween = tl as unknown as gsap.core.Tween
}

function defaultCameraPreset(mode: 'galaxy' | 'semantic' | 'city'): { target: THREE.Vector3; offset: THREE.Vector3 } {
  if (mode === 'semantic') return { target: new THREE.Vector3(0, 0, 0), offset: new THREE.Vector3(0, 20, 62) }
  if (mode === 'city') return { target: new THREE.Vector3(0, 2, 0), offset: new THREE.Vector3(34, 30, 44) }
  return { target: new THREE.Vector3(0, 0, 0), offset: new THREE.Vector3(0, 22, 54) }
}

function resetCamera() {
  const p = defaultCameraPreset(props.mode)
  flyCameraTo(p.target, p.offset, 0.9)
}

function focusCluster(clusterId: number) {
  const cl = props.mapData.clusters.find(c => c.id === clusterId)
  if (!cl) return
  const center = new THREE.Vector3(cl.x * 42, cl.y * 30, (cl.z ?? 0) * 40)
  flyCameraTo(center, new THREE.Vector3(0, 10, 28), 0.95)
}

function focusHit() {
  if (!props.searchHits.length) return
  let wx = 0, wy = 0, wz = 0, total = 0
  for (const m of interactiveMetas) {
    const sc = hitScoreForMeta(m)
    if (sc !== undefined) {
      const w = Math.max(sc, 0.1) ** 2
      wx += m.worldPos.x * w
      wy += m.worldPos.y * w
      wz += m.worldPos.z * w
      total += w
    }
  }
  if (total > 0) {
    flyCameraTo(new THREE.Vector3(wx / total, wy / total, wz / total), new THREE.Vector3(14, 14, 28), 1.05)
  }
}

defineExpose({ resetCamera, focusCluster, focusHit })

function registerMeta(meta: InteractiveMeta) {
  meta.mesh.userData.meta = meta
  interactiveMetas.push(meta)
  interactiveMeshes.push(meta.mesh)
}

function createCurveLine(p1: THREE.Vector3, p2: THREE.Vector3, arcHeight: number, colorHex: number, opacity: number, withTraveler = false) {
  if (!modeGroup) return
  const mid = p1.clone().add(p2).multiplyScalar(0.5)
  mid.y += arcHeight
  const curve = new THREE.QuadraticBezierCurve3(p1.clone(), mid, p2.clone())
  const geo = trackMode(new THREE.BufferGeometry().setFromPoints(curve.getPoints(28)))
  const mat = trackMode(new THREE.LineBasicMaterial({
    color: colorHex, transparent: true, opacity, blending: THREE.AdditiveBlending, depthWrite: false,
  }))
  modeGroup.add(new THREE.Line(geo, mat))

  if (withTraveler) {
    const tGeo = trackMode(new THREE.SphereGeometry(0.28, 8, 8))
    const tMat = trackMode(new THREE.MeshBasicMaterial({ color: colorHex }))
    const tMesh = new THREE.Mesh(tGeo, tMat)
    modeGroup.add(tMesh)
    travelers.push({ curve, mesh: tMesh, speed: 0.22 + Math.random() * 0.18, offset: Math.random() })
  }
}

function buildGalaxyScene() {
  if (!modeGroup) return
  const { mapData } = props

  // 1. 中心知识库核心枢纽
  const hubGeo = trackMode(new THREE.IcosahedronGeometry(2.8, 1))
  const hubMat = trackMode(new THREE.MeshStandardMaterial({
    color: 0x38bdf8, emissive: 0x0ea5e9, emissiveIntensity: 1.55, roughness: 0.2, metalness: 0.5,
  }))
  const hubMesh = new THREE.Mesh(hubGeo, hubMat)
  modeGroup.add(hubMesh)

  const hubWireGeo = trackMode(new THREE.IcosahedronGeometry(4.1, 1))
  const hubWireMat = trackMode(new THREE.MeshBasicMaterial({
    color: 0x7dd3fc, wireframe: true, transparent: true, opacity: 0.28,
  }))
  const hubWire = new THREE.Mesh(hubWireGeo, hubWireMat)
  modeGroup.add(hubWire)
  hubRings.push({ mesh: hubWire, sx: 0.2, sy: 0.35, sz: 0 })

  const orbitSpecs = [
    { r: 6.2, rx: 1.15, ry: 0.3, color: 0x38bdf8, sy: 0.5 },
    { r: 7.4, rx: -0.65, ry: 0.8, color: 0x818cf8, sy: -0.35 },
    { r: 16, rx: Math.PI / 2, ry: 0, color: 0x1e3a5f, sy: 0 },
    { r: 26, rx: Math.PI / 2, ry: 0, color: 0x1a3830, sy: 0 },
    { r: 35, rx: Math.PI / 2, ry: 0, color: 0x262645, sy: 0 },
  ]
  for (let idx = 0; idx < orbitSpecs.length; idx++) {
    const sp = orbitSpecs[idx]
    const ringGeo = trackMode(new THREE.TorusGeometry(sp.r, idx < 2 ? 0.065 : 0.04, 8, 96))
    const ringMat = trackMode(new THREE.MeshBasicMaterial({
      color: sp.color, transparent: true, opacity: idx < 2 ? 0.6 : 0.22, blending: THREE.AdditiveBlending,
    }))
    const ringMesh = new THREE.Mesh(ringGeo, ringMat)
    ringMesh.rotation.set(sp.rx, sp.ry, 0)
    modeGroup.add(ringMesh)
    if (sp.sy !== 0) hubRings.push({ mesh: ringMesh, sx: 0.08, sy: sp.sy, sz: 0.14 })
  }

  registerMeta({
    key: `hub:${mapData.id}`,
    kind: 'hub',
    title: mapData.name,
    sub: `${mapData.documentCount} 文档 · ${mapData.entryCount} 问答 · ${mapData.chunkCount} 切片`,
    snippet: mapData.description || '点击查看知识库整体态势与绑定范围',
    colorHex: 0x38bdf8,
    baseScale: 1,
    worldPos: new THREE.Vector3(0, 0, 0),
    selection: null,
    priority: 100,
    mesh: hubMesh,
    material: hubMat,
  })

  // 2. 内轨：文档节点 (R = 16)
  const docs = mapData.documents.slice(0, 18)
  const docGeo = trackMode(new THREE.DodecahedronGeometry(1, 0))
  docs.forEach((doc, i) => {
    const angle = (i / Math.max(docs.length, 1)) * Math.PI * 2
    const r = 16
    const pos = new THREE.Vector3(Math.cos(angle) * r, Math.sin(angle * 2) * 2.2, Math.sin(angle) * r)
    const isFailed = doc.status === 'failed'
    const isBusy = doc.status === 'processing' || doc.status === 'pending'
    const colorHex = isFailed ? 0xf87171 : isBusy ? 0xfbbf24 : 0x38bdf8
    const scale = 0.75 + Math.min(0.85, Math.sqrt(Math.max(doc.chunkCount, 1)) * 0.16)
    const mat = trackMode(new THREE.MeshStandardMaterial({
      color: colorHex, emissive: colorHex, emissiveIntensity: 1.15, roughness: 0.25, metalness: 0.3,
    }))
    const mesh = new THREE.Mesh(docGeo, mat)
    mesh.position.copy(pos)
    mesh.scale.setScalar(0.01)
    modeGroup!.add(mesh)
    gsap.to(mesh.scale, { x: scale, y: scale, z: scale, duration: 0.65, delay: i * 0.03, ease: 'back.out(1.6)' })

    createCurveLine(new THREE.Vector3(0, 0, 0), pos, 2.8, colorHex, 0.24, true)

    registerMeta({
      key: `doc:${doc.id}`,
      kind: 'document',
      title: doc.name,
      sub: `${doc.type} · ${doc.chunkCount} 分块`,
      snippet: doc.preview || doc.lastError,
      colorHex,
      baseScale: scale,
      worldPos: pos,
      selection: { type: 'document', id: doc.id },
      sourceDocId: doc.id,
      priority: 65,
      mesh,
      material: mat,
    })
  })

  // 3. 中轨：热门问答 (R = 26)
  const entries = mapData.entries.slice(0, 22)
  const entryGeo = trackMode(new THREE.OctahedronGeometry(0.95, 0))
  entries.forEach((entry, i) => {
    const angle = (i / Math.max(entries.length, 1)) * Math.PI * 2 + 0.35
    const r = 26
    const pos = new THREE.Vector3(Math.cos(angle) * r, Math.cos(angle * 3) * 3.2, Math.sin(angle) * r)
    const bad = entry.unhelpfulCount > entry.helpfulCount && entry.unhelpfulCount > 0
    const colorHex = bad ? 0xf87171 : 0x34d399
    const scale = 0.72 + Math.min(0.8, Math.sqrt(entry.hitCount + 1) * 0.16)
    const mat = trackMode(new THREE.MeshStandardMaterial({
      color: colorHex, emissive: colorHex, emissiveIntensity: 1.1, roughness: 0.25,
    }))
    const mesh = new THREE.Mesh(entryGeo, mat)
    mesh.position.copy(pos)
    mesh.scale.setScalar(0.01)
    modeGroup!.add(mesh)
    gsap.to(mesh.scale, { x: scale, y: scale, z: scale, duration: 0.65, delay: 0.12 + i * 0.025, ease: 'back.out(1.5)' })

    createCurveLine(new THREE.Vector3(0, 0, 0), pos, -2.5, colorHex, 0.16, entry.hitCount > 0)

    registerMeta({
      key: `entry:${entry.id}`,
      kind: 'entry',
      title: entry.question,
      sub: `命中 ${entry.hitCount} · 好评 ${entry.helpfulCount}${entry.unhelpfulCount ? ` · 差评 ${entry.unhelpfulCount}` : ''}`,
      snippet: entry.answer,
      colorHex,
      baseScale: scale,
      worldPos: pos,
      selection: { type: 'entry', id: entry.id },
      sourceEntryId: entry.id,
      priority: 50 + Math.min(20, entry.hitCount),
      mesh,
      material: mat,
    })
  })

  // 4. 外轨：使用方、待审核、缺口 (R = 35)
  const outerItems: Array<Omit<InteractiveMeta, 'worldPos' | 'mesh' | 'material'> & { geoKind: 'sat' | 'torus' }> = []
  for (const ag of mapData.boundAgents.slice(0, 5)) {
    outerItems.push({
      key: `agent:${ag.id}`, kind: 'agent', title: ag.name, sub: '绑定 AI 客服',
      colorHex: 0x818cf8, baseScale: 0.85, selection: null, priority: 35, geoKind: 'sat',
    })
  }
  for (const cv of mapData.boundConversations.slice(0, 8)) {
    outerItems.push({
      key: `conv:${cv.id}`, kind: 'conversation', title: cv.name, sub: cv.type === 'private' ? '私聊使用中' : 'QQ 群聊使用中',
      colorHex: 0x60a5fa, baseScale: 0.8, selection: null, priority: 38, geoKind: 'sat',
    })
  }
  for (const g of mapData.gaps.slice(0, 5)) {
    outerItems.push({
      key: `gap:${g.id}`, kind: 'gap', title: `缺口：${g.question}`, sub: `出现 ${g.occurrences} 次未答上`,
      colorHex: 0xfbbf24, baseScale: 0.95, selection: { type: 'gap', id: g.id }, priority: 58, geoKind: 'torus',
    })
  }
  for (const cand of mapData.candidates.slice(0, 4)) {
    outerItems.push({
      key: `cand:${cand.id}`, kind: 'candidate', title: `待审：${cand.question}`, sub: `可信度 ${Math.round(cand.confidence * 100)}%`,
      snippet: cand.answer, colorHex: 0xc084fc, baseScale: 0.88, selection: { type: 'candidate', id: cand.id }, priority: 52, geoKind: 'sat',
    })
  }
  if (props.unreadFeedbackCount > 0) {
    outerItems.push({
      key: 'todo:feedback', kind: 'gap', title: `${props.unreadFeedbackCount} 条未读差评`, sub: '点击打开反馈列表',
      colorHex: 0xf87171, baseScale: 1.05, tabTarget: 'feedback', priority: 60, geoKind: 'torus',
    })
  }

  const satGeo = trackMode(new THREE.BoxGeometry(1.1, 1.1, 1.1))
  const torusGeo = trackMode(new THREE.TorusGeometry(0.9, 0.25, 10, 24))
  outerItems.forEach((item, i) => {
    const angle = (i / Math.max(outerItems.length, 1)) * Math.PI * 2 + 0.7
    const r = 35
    const pos = new THREE.Vector3(Math.cos(angle) * r, Math.sin(angle * 2.5) * 4.5, Math.sin(angle) * r)
    const mat = trackMode(new THREE.MeshStandardMaterial({
      color: item.colorHex, emissive: item.colorHex, emissiveIntensity: 1.1, roughness: 0.3,
    }))
    const mesh = new THREE.Mesh(item.geoKind === 'torus' ? torusGeo : satGeo, mat)
    mesh.position.copy(pos)
    mesh.scale.setScalar(item.baseScale)
    if (item.geoKind === 'torus') mesh.rotation.x = Math.PI / 3
    modeGroup!.add(mesh)

    createCurveLine(new THREE.Vector3(0, 0, 0), pos, 4.2, item.colorHex, 0.14, false)
    registerMeta({ ...item, worldPos: pos, mesh, material: mat })
  })
}

function buildSemanticScene() {
  if (!modeGroup) return
  const { mapData } = props

  // 1. 聚类柔光星云球体
  for (const cl of mapData.clusters) {
    const colorHex = clusterPalette[((cl.colorIndex % clusterPalette.length) + clusterPalette.length) % clusterPalette.length]
    const cx = cl.x * 42
    const cy = cl.y * 30
    const cz = (cl.z ?? 0) * 40
    const rad = Math.max(6, cl.radius * 26)
    const dimmed = props.clusterFilter !== null && props.clusterFilter !== cl.id

    const cloudGeo = trackMode(new THREE.SphereGeometry(rad, 24, 24))
    const cloudMat = trackMode(new THREE.MeshBasicMaterial({
      color: colorHex, transparent: true, opacity: dimmed ? 0.015 : 0.085, blending: THREE.AdditiveBlending, depthWrite: false,
    }))
    const cloudMesh = new THREE.Mesh(cloudGeo, cloudMat)
    cloudMesh.position.set(cx, cy, cz)
    modeGroup.add(cloudMesh)

    const shellGeo = trackMode(new THREE.SphereGeometry(rad, 14, 14))
    const shellMat = trackMode(new THREE.MeshBasicMaterial({
      color: colorHex, wireframe: true, transparent: true, opacity: dimmed ? 0.02 : 0.11,
    }))
    const shell = new THREE.Mesh(shellGeo, shellMat)
    shell.position.set(cx, cy, cz)
    modeGroup.add(shell)

    registerMeta({
      key: `cluster:${cl.id}`,
      kind: 'cluster',
      title: cl.label,
      sub: `${cl.count} 个知识切片`,
      colorHex,
      baseScale: 1,
      worldPos: new THREE.Vector3(cx, cy + rad * 0.55, cz),
      clusterId: cl.id,
      priority: 75,
      mesh: shell,
      material: shellMat,
    })
  }

  // 2. 三维语义坐标节点
  const docGeo = trackMode(new THREE.SphereGeometry(0.75, 14, 14))
  const entryGeo = trackMode(new THREE.OctahedronGeometry(0.9, 0))
  const gapGeo = trackMode(new THREE.TorusGeometry(0.85, 0.25, 8, 22))
  const clusterPoints = new Map<number, THREE.Vector3[]>()

  mapData.points.forEach((pt, idx) => {
    const cl = mapData.clusters.find(c => c.id === pt.clusterId)
    const colorHex = pt.kind === 'gap'
      ? 0xfbbf24
      : clusterPalette[((cl ? cl.colorIndex : pt.clusterId) % clusterPalette.length + clusterPalette.length) % clusterPalette.length]
    const pos = new THREE.Vector3(pt.x * 42, pt.y * 30, (pt.z ?? 0) * 40)
    const dimmed = props.clusterFilter !== null && props.clusterFilter !== pt.clusterId
    const scale = pt.kind === 'entry' ? 0.9 + Math.min(0.65, Math.sqrt(pt.hitCount) * 0.14) : 0.85

    const mat = trackMode(new THREE.MeshStandardMaterial({
      color: colorHex,
      emissive: colorHex,
      emissiveIntensity: dimmed ? 0.2 : 1.2,
      transparent: dimmed,
      opacity: dimmed ? 0.25 : 1,
      roughness: 0.25,
    }))
    const geo = pt.kind === 'entry' ? entryGeo : pt.kind === 'gap' ? gapGeo : docGeo
    const mesh = new THREE.Mesh(geo, mat)
    mesh.position.copy(pos)
    mesh.scale.setScalar(0.01)
    modeGroup!.add(mesh)
    gsap.to(mesh.scale, { x: scale, y: scale, z: scale, duration: 0.55, delay: Math.min(0.4, idx * 0.004), ease: 'back.out(1.4)' })

    if (!dimmed) {
      const bucket = clusterPoints.get(pt.clusterId) || []
      if (bucket.length < 35) { bucket.push(pos); clusterPoints.set(pt.clusterId, bucket) }
    }

    registerMeta({
      key: `pt:${pt.id}`,
      kind: 'point',
      title: pt.title,
      sub: pt.kind === 'entry' ? `问答 · 命中 ${pt.hitCount}` : pt.kind === 'gap' ? `缺口 · ${pt.hitCount} 次` : `文档切片 #${pt.chunkIndex + 1}`,
      snippet: pt.snippet,
      colorHex,
      baseScale: scale,
      worldPos: pos,
      selection: { type: 'point', point: pt },
      pointId: pt.id,
      sourceDocId: pt.kind === 'document' ? pt.sourceId : undefined,
      sourceEntryId: pt.kind === 'entry' ? pt.sourceId : undefined,
      clusterId: pt.clusterId,
      priority: pt.kind === 'entry' ? 45 + Math.min(20, pt.hitCount) : 25,
      mesh,
      material: mat,
    })
  })

  // 3. 簇内近邻星座连线
  for (const [cid, vecs] of clusterPoints) {
    const colorHex = clusterPalette[((cid % clusterPalette.length) + clusterPalette.length) % clusterPalette.length]
    const linePos: number[] = []
    for (let i = 0; i < vecs.length; i++) {
      let nearest = -1
      let minD = Infinity
      for (let j = i + 1; j < vecs.length; j++) {
        const d = vecs[i].distanceToSquared(vecs[j])
        if (d < minD) { minD = d; nearest = j }
      }
      if (nearest >= 0 && minD < 480) {
        linePos.push(vecs[i].x, vecs[i].y, vecs[i].z, vecs[nearest].x, vecs[nearest].y, vecs[nearest].z)
      }
    }
    if (linePos.length) {
      const lGeo = trackMode(new THREE.BufferGeometry())
      lGeo.setAttribute('position', new THREE.Float32BufferAttribute(linePos, 3))
      const lMat = trackMode(new THREE.LineBasicMaterial({ color: colorHex, transparent: true, opacity: 0.22, blending: THREE.AdditiveBlending }))
      modeGroup.add(new THREE.LineSegments(lGeo, lMat))
    }
  }
}

function buildCityScene() {
  if (!modeGroup) return
  const { mapData } = props

  interface CityItem {
    key: string
    kind: 'document' | 'entry'
    title: string
    sub: string
    snippet?: string
    height: number
    colorHex: number
    selection: KnowledgeInspectorSelection
    sourceDocId?: string
    sourceEntryId?: string
    priority: number
  }
  const list: CityItem[] = []
  for (const doc of mapData.documents) {
    const isFailed = doc.status === 'failed'
    const isBusy = doc.status === 'pending' || doc.status === 'processing'
    let colorHex = 0x38bdf8
    if (props.colorMode === 'heat') {
      colorHex = isFailed ? 0xf87171 : 0x38bdf8
    } else if (isFailed) colorHex = 0xf87171
    else if (isBusy) colorHex = 0xfbbf24

    list.push({
      key: `doc:${doc.id}`, kind: 'document', title: doc.name, sub: `${doc.type} · ${doc.chunkCount} 块`,
      snippet: doc.preview || doc.lastError, height: 2.5 + Math.min(18, Math.sqrt(Math.max(doc.chunkCount, 1)) * 3.2),
      colorHex, selection: { type: 'document', id: doc.id }, sourceDocId: doc.id, priority: 60,
    })
  }
  for (const entry of mapData.entries.slice(0, 28)) {
    const bad = entry.unhelpfulCount > entry.helpfulCount && entry.unhelpfulCount > 0
    let colorHex = 0x34d399
    if (props.colorMode === 'heat') {
      if (bad) colorHex = 0xf87171
      else if (entry.hitCount >= 5) colorHex = 0x10b981
      else if (entry.hitCount > 0) colorHex = 0x34d399
      else colorHex = 0x64748b
    } else if (entry.status === 'review') colorHex = 0xfbbf24
    else if (bad) colorHex = 0xf87171

    list.push({
      key: `entry:${entry.id}`, kind: 'entry', title: entry.question, sub: `命中 ${entry.hitCount} · 好评 ${entry.helpfulCount}`,
      snippet: entry.answer, height: 2.2 + Math.min(18, Math.sqrt(entry.hitCount + 1) * 3.0),
      colorHex, selection: { type: 'entry', id: entry.id }, sourceEntryId: entry.id, priority: 45 + Math.min(20, entry.hitCount),
    })
  }

  const cols = Math.max(2, Math.ceil(Math.sqrt(Math.max(list.length, 1))))
  const spacing = 5.4
  const baseY = -6
  const boxGeo = trackMode(new THREE.BoxGeometry(3.2, 1, 3.2))
  const edgeGeo = trackMode(new THREE.EdgesGeometry(boxGeo))

  list.forEach((item, idx) => {
    const col = idx % cols
    const row = Math.floor(idx / cols)
    const x = (col - (cols - 1) / 2) * spacing
    const z = (row - (Math.ceil(list.length / cols) - 1) / 2) * spacing

    const group = new THREE.Group()
    group.position.set(x, baseY, z)

    const mat = trackMode(new THREE.MeshStandardMaterial({
      color: item.colorHex, emissive: item.colorHex, emissiveIntensity: 0.75, transparent: true, opacity: 0.85, roughness: 0.2,
    }))
    const bar = new THREE.Mesh(boxGeo, mat)
    bar.position.y = item.height / 2
    bar.scale.y = item.height
    group.add(bar)

    const wireMat = trackMode(new THREE.LineBasicMaterial({ color: item.colorHex, transparent: true, opacity: 0.9 }))
    const wire = new THREE.LineSegments(edgeGeo, wireMat)
    bar.add(wire)

    group.scale.y = 0.01
    modeGroup!.add(group)
    gsap.to(group.scale, { y: 1, duration: 0.7, delay: idx * 0.025, ease: 'power3.out' })

    registerMeta({
      key: item.key,
      kind: item.kind,
      title: item.title,
      sub: item.sub,
      snippet: item.snippet,
      colorHex: item.colorHex,
      baseScale: 1,
      worldPos: new THREE.Vector3(x, baseY + item.height, z),
      selection: item.selection,
      sourceDocId: item.sourceDocId,
      sourceEntryId: item.sourceEntryId,
      priority: item.priority,
      mesh: bar,
      material: mat,
    })
  })
}

function rebuildModeScene(shouldFlyDefault = false) {
  if (!modeGroup) return
  interactiveMetas.length = 0
  interactiveMeshes.length = 0
  hubRings = []
  travelers = []
  hoveredMeta.value = null
  disposeGroup(modeGroup, modeDisposables)

  if (props.mode === 'galaxy') buildGalaxyScene()
  else if (props.mode === 'semantic') buildSemanticScene()
  else buildCityScene()

  if (floorGrid) {
    floorGrid.visible = props.mode === 'city' || props.mode === 'galaxy'
    floorGrid.position.y = props.mode === 'city' ? -6.05 : -9.5
  }

  rebuildProbeLayer(false)
  if (shouldFlyDefault) resetCamera()
}

function rebuildProbeLayer(shouldFly = true) {
  if (!probeGroup) return
  probeWaves = []
  disposeGroup(probeGroup, probeDisposables)

  const hits = props.searchHits
  // 还原所有节点的发光状态
  for (const m of interactiveMetas) {
    const mat = m.material
    if (!mat || !('emissiveIntensity' in mat)) continue
    const isSel = isMetaSelected(m)
    const sc = hitScoreForMeta(m)
    if (hits.length) {
      mat.emissiveIntensity = sc !== undefined ? 2.4 : 0.25
    } else {
      mat.emissiveIntensity = isSel ? 2.1 : 1.15
    }
  }
  if (!hits.length) return

  const matched: Array<{ meta: InteractiveMeta; score: number }> = []
  for (const m of interactiveMetas) {
    const sc = hitScoreForMeta(m)
    if (sc !== undefined) matched.push({ meta: m, score: sc })
  }
  if (!matched.length) return

  let wx = 0, wy = 0, wz = 0, total = 0
  for (const item of matched) {
    const w = Math.max(item.score, 0.1) ** 2
    wx += item.meta.worldPos.x * w
    wy += item.meta.worldPos.y * w
    wz += item.meta.worldPos.z * w
    total += w
  }
  const center = new THREE.Vector3(wx / total, wy / total + 4.5, wz / total)

  // 探针核心球与扩散冲击波环
  const pGeo = trackProbe(new THREE.OctahedronGeometry(1.15, 0))
  const pMat = trackProbe(new THREE.MeshBasicMaterial({ color: 0x7dd3fc }))
  const pMesh = new THREE.Mesh(pGeo, pMat)
  pMesh.position.copy(center)
  probeGroup.add(pMesh)

  for (let i = 0; i < 2; i++) {
    const wGeo = trackProbe(new THREE.TorusGeometry(1.6, 0.08, 8, 48))
    const wMat = trackProbe(new THREE.MeshBasicMaterial({ color: 0x38bdf8, transparent: true, opacity: 0.85, blending: THREE.AdditiveBlending }))
    const wMesh = new THREE.Mesh(wGeo, wMat)
    wMesh.position.copy(center)
    wMesh.rotation.x = Math.PI / 2
    wMesh.userData.phase = i * 0.5
    probeGroup.add(wMesh)
    probeWaves.push(wMesh)
  }

  for (const item of matched) {
    const mid = center.clone().add(item.meta.worldPos).multiplyScalar(0.5)
    mid.y += 3
    const curve = new THREE.QuadraticBezierCurve3(center.clone(), mid, item.meta.worldPos.clone())
    const lGeo = trackProbe(new THREE.BufferGeometry().setFromPoints(curve.getPoints(24)))
    const lMat = trackProbe(new THREE.LineBasicMaterial({ color: 0x38bdf8, transparent: true, opacity: 0.9, blending: THREE.AdditiveBlending }))
    probeGroup.add(new THREE.Line(lGeo, lMat))
  }

  if (shouldFly) focusHit()
}

function updateHudProjection() {
  const container = containerRef.value
  if (!container || !camera) return
  const width = container.clientWidth
  const height = container.clientHeight
  if (!width || !height) return

  const candidates: Array<{ meta: InteractiveMeta; x: number; y: number; score?: number; isHit: boolean; isSelected: boolean; rank: number }> = []
  for (const m of interactiveMetas) {
    if (props.mode === 'semantic' && m.kind === 'point' && !hitScoreForMeta(m) && !isMetaSelected(m) && hoveredMeta.value?.key !== m.key) {
      continue
    }
    projVec.copy(m.worldPos).project(camera)
    if (projVec.z > 1 || projVec.z < -1) continue
    const x = (projVec.x * 0.5 + 0.5) * width
    const y = (-projVec.y * 0.5 + 0.5) * height
    if (x < 40 || x > width - 40 || y < 50 || y > height - 70) continue

    const score = hitScoreForMeta(m)
    const isHit = score !== undefined
    const isSelected = isMetaSelected(m)
    const isHover = hoveredMeta.value?.key === m.key
    const rank = m.priority + (isHit ? 200 : 0) + (isSelected ? 300 : 0) + (isHover ? 400 : 0)
    candidates.push({ meta: m, x, y, score, isHit, isSelected, rank })
  }

  candidates.sort((a, b) => b.rank - a.rank)
  const placed: HudLabel[] = []
  for (const c of candidates) {
    if (placed.length >= 16) break
    const overlaps = placed.some(p => Math.abs(p.x - c.x) < 110 && Math.abs(p.y - c.y) < 30)
    if (overlaps && !c.isHit && !c.isSelected && hoveredMeta.value?.key !== c.meta.key) continue
    placed.push({
      key: c.meta.key,
      kind: c.meta.kind,
      text: c.meta.title.length > 14 ? c.meta.title.slice(0, 14) + '…' : c.meta.title,
      x: Math.round(c.x),
      y: Math.round(c.y),
      score: c.score,
      isHit: c.isHit,
      isSelected: c.isSelected,
    })
  }
  hudLabels.value = placed
}

function updatePointerRay(e: PointerEvent): InteractiveMeta | null {
  const container = containerRef.value
  if (!container || !camera || !interactiveMeshes.length) return null
  const rect = container.getBoundingClientRect()
  pointerNDC.x = ((e.clientX - rect.left) / rect.width) * 2 - 1
  pointerNDC.y = -((e.clientY - rect.top) / rect.height) * 2 + 1
  raycaster.setFromCamera(pointerNDC, camera)
  const hits = raycaster.intersectObjects(interactiveMeshes, false)
  if (!hits.length) return null
  return (hits[0].object.userData?.meta as InteractiveMeta) || null
}

function onPointerDown(e: PointerEvent) {
  dragOrigin = { x: e.clientX, y: e.clientY, moved: false }
}

function onPointerMove(e: PointerEvent) {
  if (Math.abs(e.clientX - dragOrigin.x) > 5 || Math.abs(e.clientY - dragOrigin.y) > 5) {
    dragOrigin.moved = true
  }
  const hit = updatePointerRay(e)
  hoveredMeta.value = hit
  const container = containerRef.value
  if (container) {
    container.style.cursor = hit ? 'pointer' : 'grab'
    if (hit) {
      const rect = container.getBoundingClientRect()
      hoverScreen.x = Math.min(rect.width - 280, Math.max(16, e.clientX - rect.left + 16))
      hoverScreen.y = Math.min(rect.height - 150, Math.max(16, e.clientY - rect.top + 16))
    }
  }
}

function onPointerUp(e: PointerEvent) {
  if (dragOrigin.moved) return
  const hit = updatePointerRay(e)
  if (!hit) return
  if (hit.kind === 'cluster' && hit.clusterId !== undefined) {
    focusCluster(hit.clusterId)
    return
  }
  if (hit.tabTarget) {
    emit('navigateDrawer', hit.tabTarget)
    return
  }
  if (hit.selection !== undefined) {
    emit('select', hit.selection)
    if (hit.selection) {
      flyCameraTo(hit.worldPos, new THREE.Vector3(10, 9, 18), 0.85)
    } else {
      resetCamera()
    }
  }
}

function kindText(k: InteractiveMeta['kind']) {
  const map: Record<InteractiveMeta['kind'], string> = {
    hub: '知识库核心', document: '知识文档', entry: '标准问答',
    candidate: '待审核', gap: '知识缺口', agent: 'AI 客服',
    conversation: '绑定群聊', point: '向量切片', cluster: '语义星云簇',
  }
  return map[k]
}

watch(() => props.mode, () => rebuildModeScene(true))
watch([() => props.mapData, () => props.colorMode, () => props.clusterFilter], () => rebuildModeScene(false), { deep: false })
watch(() => props.searchHits, () => rebuildProbeLayer(true))
watch(() => props.selection, () => rebuildProbeLayer(false))
watch(() => props.autoRotate, (val) => { if (controls) controls.autoRotate = val })

onMounted(() => {
  const host = canvasHostRef.value
  const container = containerRef.value
  if (!host || !container || typeof window === 'undefined') return

  const width = Math.max(container.clientWidth, 320)
  const height = Math.max(container.clientHeight, 320)

  try {
    renderer = new THREE.WebGLRenderer({ antialias: true, powerPreference: 'high-performance' })
  } catch {
    return
  }

  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2))
  renderer.setSize(width, height)
  renderer.toneMapping = THREE.ACESFilmicToneMapping
  renderer.toneMappingExposure = 1.15
  host.appendChild(renderer.domElement)

  scene = new THREE.Scene()
  scene.background = new THREE.Color(0x040814)
  scene.fog = new THREE.FogExp2(0x040814, 0.0085)

  camera = new THREE.PerspectiveCamera(50, width / height, 0.2, 400)
  const preset = defaultCameraPreset(props.mode)
  camera.position.copy(preset.target.clone().add(preset.offset))

  controls = new OrbitControls(camera, renderer.domElement)
  controls.enableDamping = true
  controls.dampingFactor = 0.06
  controls.maxDistance = 150
  controls.minDistance = 8
  controls.autoRotate = props.autoRotate
  controls.autoRotateSpeed = 0.55

  composer = new EffectComposer(renderer)
  composer.addPass(new RenderPass(scene, camera))
  bloomPass = new UnrealBloomPass(new THREE.Vector2(width, height), 0.85, 0.45, 0.22)
  composer.addPass(bloomPass)
  composer.addPass(new OutputPass())

  // 灯光
  scene.add(new THREE.AmbientLight(0x94a3b8, 0.85))
  const mainLight = new THREE.PointLight(0x38bdf8, 80, 120, 1.5)
  mainLight.position.set(0, 18, 12)
  scene.add(mainLight)
  const fillLight = new THREE.PointLight(0x818cf8, 50, 120, 1.5)
  fillLight.position.set(-20, -10, -18)
  scene.add(fillLight)

  rootGroup = new THREE.Group()
  scene.add(rootGroup)
  modeGroup = new THREE.Group()
  rootGroup.add(modeGroup)
  probeGroup = new THREE.Group()
  rootGroup.add(probeGroup)

  // 科幻极坐标网格地板
  floorGrid = new THREE.GridHelper(130, 32, 0x1e3a5f, 0x0f1f3a)
  floorGrid.position.y = -9.5
  const gridMat = floorGrid.material as THREE.Material
  gridMat.transparent = true
  gridMat.opacity = 0.42
  rootGroup.add(floorGrid)
  trackGlobal(floorGrid)

  // 1400 颗深空背景星尘
  const starCount = 1400
  const starArr = new Float32Array(starCount * 3)
  for (let i = 0; i < starCount; i++) {
    starArr[i * 3] = (Math.random() - 0.5) * 220
    starArr[i * 3 + 1] = (Math.random() - 0.5) * 160
    starArr[i * 3 + 2] = (Math.random() - 0.5) * 220
  }
  const starGeo = trackGlobal(new THREE.BufferGeometry())
  starGeo.setAttribute('position', new THREE.BufferAttribute(starArr, 3))
  const starMat = trackGlobal(new THREE.PointsMaterial({ color: 0x94a3b8, size: 0.28, transparent: true, opacity: 0.55, depthWrite: false }))
  starField = new THREE.Points(starGeo, starMat)
  scene.add(starField)

  rebuildModeScene(false)

  if (typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(entries => {
      const rect = entries[0]?.contentRect
      if (!rect || rect.width <= 0 || rect.height <= 0 || !renderer || !composer || !camera) return
      const w = Math.floor(rect.width)
      const h = Math.floor(rect.height)
      camera.aspect = w / h
      camera.updateProjectionMatrix()
      renderer.setSize(w, h)
      composer.setSize(w, h)
      bloomPass?.setSize(w, h)
    })
    resizeObserver.observe(container)
  }

  const clock = new THREE.Clock()
  const animate = () => {
    rafId = window.requestAnimationFrame(animate)
    const dt = clock.getDelta()
    const elapsed = clock.elapsedTime

    for (const r of hubRings) {
      r.mesh.rotation.x += dt * r.sx
      r.mesh.rotation.y += dt * r.sy
      r.mesh.rotation.z += dt * r.sz
    }
    for (const tr of travelers) {
      const t = (elapsed * tr.speed + tr.offset) % 1
      tr.curve.getPoint(t, tr.mesh.position)
    }
    for (const w of probeWaves) {
      const t = (elapsed * 0.65 + (w.userData.phase || 0)) % 1
      const s = 0.5 + t * 5.2
      w.scale.setScalar(s)
      ;(w.material as THREE.MeshBasicMaterial).opacity = (1 - t) * 0.8
    }
    if (starField) {
      starField.rotation.y += dt * 0.008
    }

    controls?.update()
    composer?.render()
    frameCounter++
    if (frameCounter % 2 === 0) updateHudProjection()
  }
  animate()
})

onBeforeUnmount(() => {
  activeTween?.kill()
  if (rafId) window.cancelAnimationFrame(rafId)
  resizeObserver?.disconnect()
  disposeGroup(modeGroup, modeDisposables)
  disposeGroup(probeGroup, probeDisposables)
  for (const d of globalDisposables) d.dispose()
  globalDisposables.length = 0
  controls?.dispose()
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
.universe-3d { position: relative; width: 100%; height: 100%; overflow: hidden; background: #040814; user-select: none; }
.webgl-host { position: absolute; inset: 0; }
.webgl-host :deep(canvas) { display: block; width: 100% !important; height: 100% !important; }

.hud-labels { position: absolute; inset: 0; pointer-events: none; overflow: hidden; z-index: 2; }
.hud-tag {
  position: absolute;
  top: 0;
  left: 0;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 2px 8px;
  border: 1px solid rgba(148, 163, 184, 0.24);
  border-radius: 999px;
  background: rgba(8, 14, 28, 0.72);
  color: #dbeafe;
  font-size: 11px;
  line-height: 1.4;
  white-space: nowrap;
  backdrop-filter: blur(8px);
  will-change: transform;
}
.hud-tag .dot { width: 6px; height: 6px; border-radius: 50%; background: #38bdf8; }
.hud-tag.kind-entry .dot { background: #34d399; }
.hud-tag.kind-gap .dot { background: #fbbf24; }
.hud-tag.kind-cluster .dot { background: #818cf8; }
.hud-tag.kind-hub { border-color: rgba(56, 189, 248, 0.55); background: rgba(14, 26, 52, 0.88); font-weight: 600; color: #f0f9ff; }
.hud-tag.is-hit, .hud-tag.is-selected { border-color: #38bdf8; background: rgba(14, 165, 233, 0.28); color: #ffffff; box-shadow: 0 0 14px rgba(56, 189, 248, 0.45); }
.hud-tag .score { font-style: normal; font-weight: 700; color: #7dd3fc; font-variant-numeric: tabular-nums; }

.hover-card {
  position: absolute;
  z-index: 6;
  width: 264px;
  padding: 10px 12px;
  border: 1px solid rgba(56, 189, 248, 0.35);
  border-radius: 10px;
  background: rgba(9, 15, 30, 0.92);
  box-shadow: 0 14px 36px rgba(0, 0, 0, 0.6);
  backdrop-filter: blur(14px);
  pointer-events: none;
}
.hover-head { display: flex; align-items: center; gap: 6px; margin-bottom: 4px; }
.hover-kind { flex: none; padding: 1px 6px; border-radius: 4px; background: rgba(56, 189, 248, 0.18); color: #7dd3fc; font-size: 10px; font-weight: 600; }
.hover-head strong { color: #f1f5f9; font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.hover-card small { display: block; color: #94a3b8; font-size: 11px; }
.hover-card p { margin: 6px 0 0; color: #cbd5e1; font-size: 12px; line-height: 1.5; display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden; }

.universe-empty {
  position: absolute;
  inset: 0;
  z-index: 3;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  text-align: center;
  pointer-events: none;
}
.empty-orb { width: 68px; height: 68px; border-radius: 50%; background: radial-gradient(circle, rgba(56, 189, 248, 0.55), transparent 70%); border: 1px dashed rgba(56, 189, 248, 0.45); animation: pulse-orb 3s ease-in-out infinite; }
.universe-empty h3 { margin: 4px 0 0; color: #e2e8f0; font-size: 16px; font-weight: 600; }
.universe-empty p { max-width: 42ch; color: #94a3b8; font-size: 13px; }
@keyframes pulse-orb { 0%, 100% { transform: scale(1); opacity: 0.7; } 50% { transform: scale(1.12); opacity: 1; } }
</style>
