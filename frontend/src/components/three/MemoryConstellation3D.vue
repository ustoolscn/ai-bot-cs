<template>
  <div ref="containerRef" class="mem-universe" @pointermove="onPointerMove" @pointerleave="onPointerLeave" @click="onCanvasClick">
    <!-- 3D→2D 屏幕投影人物浮标 -->
    <div class="hud-tags-layer" aria-hidden="true">
      <button
        v-for="tag in hudTags"
        :key="tag.id"
        type="button"
        class="hud-tag"
        :class="[`stage-${tag.stage}`, { 'is-selected': tag.id === selectedId, 'is-hovered': tag.id === hovered?.id }]"
        :style="{ transform: `translate3d(${tag.x}px, ${tag.y}px, 0) translate(-50%, -100%)`, opacity: tag.opacity }"
        @click.stop="selectById(tag.id)"
      >
        <i class="dot" />
        <span class="label">{{ tag.name }}</span>
        <span v-if="tag.union" class="union-mark" title="UnionID 已贯通">U</span>
        <em class="count">{{ tag.memories }}</em>
      </button>
    </div>

    <!-- 悬停人物全息卡片 -->
    <Transition name="inspector-swap">
      <div v-if="hovered" class="hover-card" :style="{ left: `${hoverPos.x}px`, top: `${hoverPos.y}px` }">
        <div class="hover-head">
          <span class="stage-chip" :class="`stage-${hovered.relationshipStage}`">{{ stageNames[hovered.relationshipStage] || hovered.relationshipStage }}</span>
          <strong>{{ hovered.displayName || '未命名用户' }}</strong>
        </div>
        <p class="hover-sub">
          互动 {{ hovered.interactionTurns }} 轮 · {{ hovered.memoryCount }} 条记忆 · {{ hovered.links.length }} 个身份锚点
        </p>
        <div class="hover-bars">
          <span>熟悉 {{ Math.round(hovered.familiarity) }}</span>
          <span>亲近 {{ Math.round(hovered.affinity) }}</span>
          <span>信任 {{ Math.round(hovered.trust) }}</span>
        </div>
      </div>
    </Transition>
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
import gsap from 'gsap'
import type { SubjectSummary } from '@/api/agent'

const props = defineProps<{
  subjects: SubjectSummary[]
  selectedId?: string
}>()

const emit = defineEmits<{
  (e: 'select', subject: SubjectSummary): void
}>()

interface HudTag {
  id: string
  name: string
  stage: string
  memories: number
  union: boolean
  x: number
  y: number
  opacity: number
}

const stageNames: Record<string, string> = {
  close_friend: '老铁',
  familiar: '熟人',
  acquaintance: '脸熟',
  stranger: '初识',
}

const stageOrbitRadius: Record<string, number> = {
  close_friend: 4.0,
  familiar: 6.8,
  acquaintance: 9.8,
  stranger: 13.0,
}

const stageColors: Record<string, number> = {
  close_friend: 0xfbbf24,
  familiar: 0x34d399,
  acquaintance: 0x38bdf8,
  stranger: 0x818cf8,
}

const containerRef = ref<HTMLElement>()
const hovered = ref<SubjectSummary | null>(null)
const hoverPos = ref({ x: 0, y: 0 })
const hudTags = ref<HudTag[]>([])

let renderer: THREE.WebGLRenderer | null = null
let composer: EffectComposer | null = null
let bloomPass: UnrealBloomPass | null = null
let scene: THREE.Scene | null = null
let camera: THREE.PerspectiveCamera | null = null
let controls: OrbitControls | null = null
let resizeObserver: ResizeObserver | null = null
let rafId = 0

let coreGroup: THREE.Group | null = null
let innerCore: THREE.Mesh | null = null
let outerShell: THREE.Mesh | null = null
let subjectGroup: THREE.Group | null = null
let highlightRing: THREE.Mesh | null = null
let bgStars: THREE.Points | null = null

const raycaster = new THREE.Raycaster()
const pointer = new THREE.Vector2(-9, -9)
const pickableMeshes: THREE.Mesh[] = []
const meshToSubject = new Map<THREE.Object3D, SubjectSummary>()
const subjectToNodePos = new Map<string, THREE.Vector3>()
const animatedOrbits: Array<{ mesh: THREE.Object3D; basePos: THREE.Vector3; speed: number; phase: number; amp: number }> = []
const sceneDisposables: Array<{ dispose: () => void }> = []
const dynamicDisposables: Array<{ dispose: () => void }> = []

function trackStatic<T extends { dispose: () => void }>(item: T): T {
  sceneDisposables.push(item)
  return item
}

function trackDynamic<T extends { dispose: () => void }>(item: T): T {
  dynamicDisposables.push(item)
  return item
}

function clearDynamic() {
  if (subjectGroup) {
    while (subjectGroup.children.length > 0) {
      subjectGroup.remove(subjectGroup.children[0])
    }
  }
  for (const item of dynamicDisposables) item.dispose()
  dynamicDisposables.length = 0
  pickableMeshes.length = 0
  meshToSubject.clear()
  subjectToNodePos.clear()
  animatedOrbits.length = 0
}

function rebuildConstellation() {
  if (!subjectGroup) return
  clearDynamic()

  const list = props.subjects || []
  const byStage: Record<string, SubjectSummary[]> = {
    close_friend: [],
    familiar: [],
    acquaintance: [],
    stranger: [],
  }
  for (const s of list) {
    const key = byStage[s.relationshipStage] ? s.relationshipStage : 'stranger'
    byStage[key].push(s)
  }

  let globalIndex = 0
  for (const [stage, items] of Object.entries(byStage)) {
    const baseRadius = stageOrbitRadius[stage] || 11.5
    const colorHex = stageColors[stage] || 0x818cf8
    const count = items.length
    items.forEach((subj, idx) => {
      globalIndex++
      const angle = (idx / Math.max(count, 1)) * Math.PI * 2 + (stage.length * 0.42)
      const jitterR = baseRadius + ((idx % 3) - 1) * 0.55
      const y = Math.sin(angle * 2 + globalIndex * 0.65) * 1.15
      const pos = new THREE.Vector3(Math.cos(angle) * jitterR, y, Math.sin(angle) * jitterR)

      const size = Math.min(0.86, 0.3 + Math.sqrt(Math.max(1, subj.memoryCount + subj.interactionTurns * 0.25)) * 0.065)
      const geo = trackDynamic(new THREE.SphereGeometry(size, 22, 22))
      const isSelected = subj.id === props.selectedId
      const mat = trackDynamic(new THREE.MeshBasicMaterial({
        color: isSelected ? 0xffffff : colorHex,
        transparent: true,
        opacity: isSelected ? 0.98 : 0.88,
      }))
      const sphere = new THREE.Mesh(geo, mat)
      sphere.position.copy(pos)
      subjectGroup!.add(sphere)

      // 发光外晕
      const haloGeo = trackDynamic(new THREE.SphereGeometry(size * 1.38, 14, 14))
      const haloMat = trackDynamic(new THREE.MeshBasicMaterial({
        color: colorHex,
        transparent: true,
        opacity: isSelected ? 0.34 : 0.16,
        blending: THREE.AdditiveBlending,
        depthWrite: false,
      }))
      sphere.add(new THREE.Mesh(haloGeo, haloMat))

      // UnionID 全局贯通标识环
      if (subj.links.some(l => l.identityType === 'qq_union_id')) {
        const ringGeo = trackDynamic(new THREE.TorusGeometry(size * 1.75, 0.032, 8, 36))
        const ringMat = trackDynamic(new THREE.MeshBasicMaterial({
          color: 0xfbbf24,
          transparent: true,
          opacity: 0.78,
          blending: THREE.AdditiveBlending,
        }))
        const ring = new THREE.Mesh(ringGeo, ringMat)
        ring.rotation.set(1.05, 0.35, 0)
        sphere.add(ring)
      }

      // 能量连线到中心认知核
      const linkPoints = [new THREE.Vector3(0, 0, 0), pos]
      const linkGeo = trackDynamic(new THREE.BufferGeometry().setFromPoints(linkPoints))
      const hasMultiBots = new Set(subj.links.map(l => l.botId)).size > 1
      const linkMat = trackDynamic(new THREE.LineBasicMaterial({
        color: hasMultiBots ? 0x38bdf8 : colorHex,
        transparent: true,
        opacity: isSelected ? 0.65 : hasMultiBots ? 0.32 : 0.14,
        blending: THREE.AdditiveBlending,
        depthWrite: false,
      }))
      subjectGroup!.add(new THREE.Line(linkGeo, linkMat))

      pickableMeshes.push(sphere)
      meshToSubject.set(sphere, subj)
      subjectToNodePos.set(subj.id, pos)
      animatedOrbits.push({ mesh: sphere, basePos: pos.clone(), speed: 0.9 + (idx % 4) * 0.25, phase: globalIndex * 0.7, amp: 0.16 })
    })
  }

  syncHighlight()
}

function syncHighlight() {
  if (!highlightRing) return
  const pos = props.selectedId ? subjectToNodePos.get(props.selectedId) : undefined
  if (!pos) {
    highlightRing.visible = false
    return
  }
  highlightRing.visible = true
  highlightRing.position.copy(pos)
}

function focusOnSubject(id?: string) {
  if (!id || !camera || !controls) return
  const pos = subjectToNodePos.get(id)
  if (!pos) return
  syncHighlight()
  const dir = pos.clone().normalize()
  if (dir.lengthSq() < 0.001) dir.set(0, 0.3, 1)
  const targetCam = pos.clone().add(dir.multiplyScalar(6.5)).add(new THREE.Vector3(0, 2.2, 0))
  gsap.to(camera.position, { x: targetCam.x, y: targetCam.y, z: targetCam.z, duration: 0.85, ease: 'power3.out' })
  gsap.to(controls.target, { x: pos.x * 0.65, y: pos.y * 0.65, z: pos.z * 0.65, duration: 0.85, ease: 'power3.out' })
}

function resetCamera() {
  if (!camera || !controls) return
  gsap.to(camera.position, { x: 0, y: 12.5, z: 22, duration: 0.9, ease: 'power3.out' })
  gsap.to(controls.target, { x: 0, y: 0, z: 0, duration: 0.9, ease: 'power3.out' })
}

function selectById(id: string) {
  const subj = props.subjects.find(s => s.id === id)
  if (!subj) return
  emit('select', subj)
  focusOnSubject(subj.id)
}

function updatePointer(e: PointerEvent) {
  const host = containerRef.value
  if (!host) return
  const rect = host.getBoundingClientRect()
  pointer.x = ((e.clientX - rect.left) / Math.max(rect.width, 1)) * 2 - 1
  pointer.y = -((e.clientY - rect.top) / Math.max(rect.height, 1)) * 2 + 1
  hoverPos.value = {
    x: Math.min(rect.width - 240, Math.max(12, e.clientX - rect.left + 16)),
    y: Math.min(rect.height - 110, Math.max(12, e.clientY - rect.top - 12)),
  }
}

function onPointerMove(e: PointerEvent) {
  updatePointer(e)
  if (!camera || !pickableMeshes.length) return
  raycaster.setFromCamera(pointer, camera)
  const hits = raycaster.intersectObjects(pickableMeshes, false)
  if (hits.length > 0) {
    const subj = meshToSubject.get(hits[0].object) || null
    hovered.value = subj
    if (containerRef.value) containerRef.value.style.cursor = subj ? 'pointer' : 'grab'
  } else {
    hovered.value = null
    if (containerRef.value) containerRef.value.style.cursor = 'grab'
  }
}

function onPointerLeave() {
  hovered.value = null
  pointer.set(-9, -9)
}

function onCanvasClick() {
  if (hovered.value) {
    emit('select', hovered.value)
    focusOnSubject(hovered.value.id)
  }
}

function updateHudTags() {
  if (!camera || !containerRef.value) return
  const width = containerRef.value.clientWidth
  const height = containerRef.value.clientHeight
  if (!width || !height) return

  const next: HudTag[] = []
  const tempV = new THREE.Vector3()
  const maxShow = 28

  for (const [mesh, subj] of meshToSubject.entries()) {
    if (next.length >= maxShow && subj.id !== props.selectedId && subj.id !== hovered.value?.id) continue
    mesh.getWorldPosition(tempV)
    tempV.y += 0.72
    tempV.project(camera)
    if (tempV.z > 1 || tempV.z < -1) continue
    const sx = (tempV.x * 0.5 + 0.5) * width
    const sy = (-tempV.y * 0.5 + 0.5) * height
    if (sx < 40 || sx > width - 40 || sy < 40 || sy > height - 30) continue
    const dist = camera.position.distanceTo(mesh.position)
    const opacity = subj.id === props.selectedId || subj.id === hovered.value?.id ? 1 : Math.max(0.42, Math.min(0.95, 1.3 - dist / 34))
    next.push({
      id: subj.id,
      name: (subj.displayName || '未命名').slice(0, 12),
      stage: subj.relationshipStage,
      memories: subj.memoryCount,
      union: subj.links.some(l => l.identityType === 'qq_union_id'),
      x: Math.round(sx),
      y: Math.round(sy),
      opacity: Number(opacity.toFixed(2)),
    })
  }
  hudTags.value = next
}

watch(() => props.subjects, rebuildConstellation, { deep: true })
watch(() => props.selectedId, (id) => {
  rebuildConstellation()
  if (id) focusOnSubject(id)
})

onMounted(() => {
  const host = containerRef.value
  if (!host || typeof window === 'undefined') return
  const width = Math.max(host.clientWidth, 320)
  const height = Math.max(host.clientHeight, 320)

  try {
    renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true, powerPreference: 'high-performance' })
  } catch {
    return
  }

  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2))
  renderer.setSize(width, height)
  renderer.toneMapping = THREE.ACESFilmicToneMapping
  renderer.toneMappingExposure = 1.15
  host.prepend(renderer.domElement)

  scene = new THREE.Scene()
  scene.fog = new THREE.FogExp2(0x040814, 0.018)

  camera = new THREE.PerspectiveCamera(48, width / height, 0.1, 180)
  camera.position.set(0, 12.5, 22)

  controls = new OrbitControls(camera, renderer.domElement)
  controls.enableDamping = true
  controls.dampingFactor = 0.06
  controls.maxDistance = 44
  controls.minDistance = 5
  controls.autoRotate = true
  controls.autoRotateSpeed = 0.35

  composer = new EffectComposer(renderer)
  composer.addPass(new RenderPass(scene, camera))
  bloomPass = new UnrealBloomPass(new THREE.Vector2(width, height), 0.72, 0.45, 0.24)
  composer.addPass(bloomPass)
  composer.addPass(new OutputPass())

  // 1. 中心全局认知核
  coreGroup = new THREE.Group()
  scene.add(coreGroup)

  const innerGeo = trackStatic(new THREE.OctahedronGeometry(1.15, 1))
  const innerMat = trackStatic(new THREE.MeshBasicMaterial({ color: 0x38bdf8, wireframe: true, transparent: true, opacity: 0.7 }))
  innerCore = new THREE.Mesh(innerGeo, innerMat)
  coreGroup.add(innerCore)

  const outerGeo = trackStatic(new THREE.IcosahedronGeometry(1.85, 1))
  const outerMat = trackStatic(new THREE.MeshBasicMaterial({ color: 0x818cf8, wireframe: true, transparent: true, opacity: 0.28 }))
  outerShell = new THREE.Mesh(outerGeo, outerMat)
  coreGroup.add(outerShell)

  // 2. 四层关系阶段星轨
  for (const [stage, radius] of Object.entries(stageOrbitRadius)) {
    const orbitGeo = trackStatic(new THREE.RingGeometry(radius - 0.035, radius + 0.035, 96))
    const orbitMat = trackStatic(new THREE.MeshBasicMaterial({
      color: stageColors[stage],
      side: THREE.DoubleSide,
      transparent: true,
      opacity: 0.26,
      blending: THREE.AdditiveBlending,
    }))
    const orbit = new THREE.Mesh(orbitGeo, orbitMat)
    orbit.rotation.x = Math.PI / 2
    scene.add(orbit)
  }

  // 3. 选中人物光环
  const hiGeo = trackStatic(new THREE.RingGeometry(0.95, 1.15, 48))
  const hiMat = trackStatic(new THREE.MeshBasicMaterial({
    color: 0xffffff,
    side: THREE.DoubleSide,
    transparent: true,
    opacity: 0.85,
    blending: THREE.AdditiveBlending,
  }))
  highlightRing = new THREE.Mesh(hiGeo, hiMat)
  highlightRing.visible = false
  scene.add(highlightRing)

  // 4. 远景星海
  const starCount = 950
  const starPos = new Float32Array(starCount * 3)
  for (let i = 0; i < starCount; i++) {
    starPos[i * 3] = (Math.random() - 0.5) * 90
    starPos[i * 3 + 1] = (Math.random() - 0.5) * 60
    starPos[i * 3 + 2] = (Math.random() - 0.5) * 90
  }
  const starGeo = trackStatic(new THREE.BufferGeometry())
  starGeo.setAttribute('position', new THREE.BufferAttribute(starPos, 3))
  const starMat = trackStatic(new THREE.PointsMaterial({ color: 0x94a3b8, size: 0.08, transparent: true, opacity: 0.45, depthWrite: false }))
  bgStars = new THREE.Points(starGeo, starMat)
  scene.add(bgStars)

  subjectGroup = new THREE.Group()
  scene.add(subjectGroup)
  rebuildConstellation()

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
    resizeObserver.observe(host)
  }

  const clock = new THREE.Clock()
  let hudTick = 0
  const animate = () => {
    rafId = window.requestAnimationFrame(animate)
    const dt = clock.getDelta()
    const t = clock.elapsedTime

    if (innerCore) {
      innerCore.rotation.y += dt * 0.45
      innerCore.rotation.x += dt * 0.22
    }
    if (outerShell) {
      outerShell.rotation.y -= dt * 0.2
    }
    if (bgStars) {
      bgStars.rotation.y += dt * 0.015
    }
    for (const o of animatedOrbits) {
      o.mesh.position.y = o.basePos.y + Math.sin(t * o.speed + o.phase) * o.amp
    }
    if (highlightRing?.visible) {
      highlightRing.lookAt(camera!.position)
      const s = 1 + Math.sin(t * 4.2) * 0.12
      highlightRing.scale.setScalar(s)
    }

    controls?.update()
    composer?.render()

    hudTick++
    if (hudTick % 2 === 0) updateHudTags()
  }
  animate()
})

onBeforeUnmount(() => {
  if (rafId) window.cancelAnimationFrame(rafId)
  resizeObserver?.disconnect()
  controls?.dispose()
  clearDynamic()
  for (const item of sceneDisposables) item.dispose()
  sceneDisposables.length = 0
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

defineExpose({ resetCamera, focusOnSubject })
</script>

<style scoped>
.mem-universe {
  position: relative;
  width: 100%;
  height: 100%;
  min-height: 520px;
  overflow: hidden;
  background: radial-gradient(circle at center, #09132a 0%, #040814 72%, #02040a 100%);
  cursor: grab;
}
.mem-universe :deep(canvas) {
  display: block;
  width: 100% !important;
  height: 100% !important;
}
.hud-tags-layer {
  position: absolute;
  inset: 0;
  z-index: 4;
  pointer-events: none;
  overflow: hidden;
}
.hud-tag {
  position: absolute;
  left: 0;
  top: 0;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 2px 8px;
  border: 1px solid rgba(148, 163, 184, 0.28);
  border-radius: 999px;
  background: rgba(8, 14, 28, 0.84);
  color: #e2e8f0;
  font-size: 11px;
  white-space: nowrap;
  pointer-events: auto;
  cursor: pointer;
  backdrop-filter: blur(8px);
  transition: border-color 140ms, background 140ms, box-shadow 140ms;
}
.hud-tag .dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #818cf8;
}
.hud-tag.stage-close_friend .dot { background: #fbbf24; box-shadow: 0 0 8px #fbbf24; }
.hud-tag.stage-familiar .dot { background: #34d399; box-shadow: 0 0 8px #34d399; }
.hud-tag.stage-acquaintance .dot { background: #38bdf8; box-shadow: 0 0 8px #38bdf8; }
.hud-tag .union-mark {
  padding: 0 4px;
  border-radius: 4px;
  background: rgba(251, 191, 36, 0.2);
  color: #fde68a;
  font-size: 10px;
  font-weight: 700;
}
.hud-tag .count {
  font-style: normal;
  color: #94a3b8;
  font-variant-numeric: tabular-nums;
}
.hud-tag.is-selected,
.hud-tag.is-hovered,
.hud-tag:hover {
  border-color: #38bdf8;
  background: rgba(14, 165, 233, 0.28);
  color: #ffffff;
  box-shadow: 0 0 14px rgba(56, 189, 248, 0.45);
}
.hover-card {
  position: absolute;
  z-index: 8;
  width: 228px;
  padding: 10px 12px;
  border: 1px solid rgba(56, 189, 248, 0.36);
  border-radius: 10px;
  background: rgba(8, 14, 28, 0.94);
  box-shadow: 0 12px 28px rgba(0, 0, 0, 0.6);
  pointer-events: none;
  backdrop-filter: blur(12px);
}
.hover-head {
  display: flex;
  align-items: center;
  gap: 6px;
}
.hover-head strong {
  color: #f8fafc;
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.stage-chip {
  flex: none;
  padding: 1px 6px;
  border-radius: 999px;
  font-size: 10px;
  font-weight: 600;
  background: rgba(129, 140, 248, 0.2);
  color: #c7d2fe;
}
.stage-chip.stage-close_friend { background: rgba(251, 191, 36, 0.2); color: #fde68a; }
.stage-chip.stage-familiar { background: rgba(52, 211, 153, 0.2); color: #6ee7b7; }
.stage-chip.stage-acquaintance { background: rgba(56, 189, 248, 0.2); color: #7dd3fc; }
.hover-sub {
  margin: 5px 0 6px;
  color: #94a3b8;
  font-size: 11px;
}
.hover-bars {
  display: flex;
  justify-content: space-between;
  font-size: 11px;
  color: #cbd5e1;
  font-variant-numeric: tabular-nums;
}
</style>
