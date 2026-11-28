<template>
  <div ref="hostRef" class="infra-canvas">
    <div class="infra-labels" aria-hidden="true">
      <div v-for="tag in labels" :key="tag.key" class="infra-tag" :class="tag.tone" :style="{ transform: `translate3d(${tag.x}px, ${tag.y}px, 0) translate(-50%, -120%)` }">
        <i /><strong>{{ tag.title }}</strong><span v-if="tag.badge">{{ tag.badge }}</span>
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

const props = defineProps<{
  queues: { inbox: number; outbox: number; knowledge: number; failed: number }
  activeCount: number
  connectionLost: boolean
}>()

interface Tag {
  key: string
  title: string
  badge: string
  tone: string
  x: number
  y: number
}

const hostRef = ref<HTMLElement>()
const labels = ref<Tag[]>([])

let renderer: THREE.WebGLRenderer | null = null
let composer: EffectComposer | null = null
let bloomPass: UnrealBloomPass | null = null
let scene: THREE.Scene | null = null
let camera: THREE.PerspectiveCamera | null = null
let controls: OrbitControls | null = null
let resizeObserver: ResizeObserver | null = null
let rafId = 0

let gatewayMat: THREE.MeshBasicMaterial | null = null
let alertRing: THREE.Mesh | null = null
const animatedTowers: THREE.Object3D[] = []
const pulses: Array<{ curve: THREE.CatmullRomCurve3; mesh: THREE.Mesh; speed: number; offset: number }> = []
const disposables: Array<{ dispose: () => void }> = []

function track<T extends { dispose: () => void }>(item: T): T {
  disposables.push(item)
  return item
}

function updateStateVisuals() {
  if (gatewayMat) {
    gatewayMat.color.setHex(props.connectionLost ? 0xf87171 : 0x38bdf8)
  }
  if (alertRing) {
    alertRing.visible = props.connectionLost || props.queues.failed > 0
  }
}

watch(() => [props.queues, props.activeCount, props.connectionLost], updateStateVisuals, { deep: true })

const anchorMeta = [
  { key: 'gw', title: '后端网关引擎', pos: new THREE.Vector3(-4.8, 2.35, 1.2) },
  { key: 'db', title: 'PostgreSQL + pgvector', pos: new THREE.Vector3(0, 2.8, -2.4) },
  { key: 'fs', title: 'DATA_DIR 文件矩阵', pos: new THREE.Vector3(4.8, 2.35, 1.2) },
]

function updateLabels() {
  if (!camera || !hostRef.value) return
  const w = hostRef.value.clientWidth
  const h = hostRef.value.clientHeight
  if (!w || !h) return
  const v = new THREE.Vector3()
  const badges = [
    props.connectionLost ? '连接异常' : `队列 ${props.queues.inbox + props.activeCount}`,
    props.queues.failed > 0 ? `失败 ${props.queues.failed}` : `索引 ${props.queues.knowledge}`,
    `待发送 ${props.queues.outbox}`,
  ]
  const tones = [
    props.connectionLost ? 'danger' : 'cyan',
    props.queues.failed > 0 ? 'warn' : 'green',
    'purple',
  ]
  labels.value = anchorMeta.map((a, i) => {
    v.copy(a.pos).project(camera!)
    return {
      key: a.key,
      title: a.title,
      badge: badges[i],
      tone: tones[i],
      x: Math.round((v.x * 0.5 + 0.5) * w),
      y: Math.round((-v.y * 0.5 + 0.5) * h),
    }
  })
}

onMounted(() => {
  const host = hostRef.value
  if (!host || typeof window === 'undefined') return
  const w = Math.max(host.clientWidth, 320)
  const h = Math.max(host.clientHeight, 220)

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
  camera = new THREE.PerspectiveCamera(42, w / h, 0.1, 100)
  camera.position.set(0, 5.2, 12.6)

  controls = new OrbitControls(camera, renderer.domElement)
  controls.enableDamping = true
  controls.enableZoom = false
  controls.minPolarAngle = Math.PI * 0.2
  controls.maxPolarAngle = Math.PI * 0.55

  composer = new EffectComposer(renderer)
  composer.addPass(new RenderPass(scene, camera))
  bloomPass = new UnrealBloomPass(new THREE.Vector2(w, h), 0.74, 0.45, 0.22)
  composer.addPass(bloomPass)
  composer.addPass(new OutputPass())

  const grid = track(new THREE.GridHelper(22, 22, 0x1e3a5f, 0x0c192f))
  grid.position.y = -1.6
  scene.add(grid)

  // 1. 网关核心塔
  const gwGeo = track(new THREE.CylinderGeometry(0.95, 1.15, 2.4, 6, 1, true))
  gatewayMat = track(new THREE.MeshBasicMaterial({ color: 0x38bdf8, wireframe: true, transparent: true, opacity: 0.78 }))
  const gwTower = new THREE.Mesh(gwGeo, gatewayMat)
  gwTower.position.set(-4.8, -0.2, 1.2)
  scene.add(gwTower)
  animatedTowers.push(gwTower)

  // 2. 数据库多层圆盘塔
  const dbGroup = new THREE.Group()
  dbGroup.position.set(0, -0.2, -2.4)
  for (let i = 0; i < 4; i++) {
    const discGeo = track(new THREE.TorusGeometry(1.25 - i * 0.12, 0.055, 8, 36))
    const discMat = track(new THREE.MeshBasicMaterial({ color: 0x34d399, transparent: true, opacity: 0.78 - i * 0.1 }))
    const disc = new THREE.Mesh(discGeo, discMat)
    disc.rotation.x = Math.PI / 2
    disc.position.y = (i - 1.5) * 0.62
    dbGroup.add(disc)
  }
  scene.add(dbGroup)
  animatedTowers.push(dbGroup)

  // 3. 文件矩阵立方塔
  const fsGroup = new THREE.Group()
  fsGroup.position.set(4.8, -0.2, 1.2)
  for (let i = 0; i < 3; i++) {
    const boxGeo = track(new THREE.BoxGeometry(1.4, 0.55, 1.4))
    const boxMat = track(new THREE.MeshBasicMaterial({ color: 0x818cf8, wireframe: true, transparent: true, opacity: 0.75 }))
    const box = new THREE.Mesh(boxGeo, boxMat)
    box.position.y = (i - 1) * 0.78
    fsGroup.add(box)
  }
  scene.add(fsGroup)
  animatedTowers.push(fsGroup)

  // 4. 塔间能量管线
  const pipeSpecs = [
    { pts: [new THREE.Vector3(-4.8, 0.2, 1.2), new THREE.Vector3(-2.4, 1.3, -0.8), new THREE.Vector3(0, 0.4, -2.4)], color: 0x38bdf8, speed: 0.55 },
    { pts: [new THREE.Vector3(0, 0.4, -2.4), new THREE.Vector3(2.4, 1.3, -0.8), new THREE.Vector3(4.8, 0.2, 1.2)], color: 0x818cf8, speed: 0.45 },
    { pts: [new THREE.Vector3(4.8, -0.4, 1.2), new THREE.Vector3(0, 0.6, 2.2), new THREE.Vector3(-4.8, -0.4, 1.2)], color: 0x34d399, speed: 0.48 },
  ]
  for (const spec of pipeSpecs) {
    const curve = new THREE.CatmullRomCurve3(spec.pts)
    const geo = track(new THREE.BufferGeometry().setFromPoints(curve.getPoints(32)))
    const mat = track(new THREE.LineBasicMaterial({ color: spec.color, transparent: true, opacity: 0.48, blending: THREE.AdditiveBlending }))
    scene.add(new THREE.Line(geo, mat))

    for (let k = 0; k < 2; k++) {
      const pGeo = track(new THREE.SphereGeometry(0.11, 8, 8))
      const pMat = track(new THREE.MeshBasicMaterial({ color: spec.color }))
      const pMesh = new THREE.Mesh(pGeo, pMat)
      scene.add(pMesh)
      pulses.push({ curve, mesh: pMesh, speed: spec.speed, offset: k * 0.5 })
    }
  }

  // 5. 故障预警脉冲环
  const alertGeo = track(new THREE.RingGeometry(2.6, 2.85, 64))
  const alertMat = track(new THREE.MeshBasicMaterial({ color: 0xf87171, side: THREE.DoubleSide, transparent: true, opacity: 0.85, blending: THREE.AdditiveBlending }))
  alertRing = new THREE.Mesh(alertGeo, alertMat)
  alertRing.rotation.x = Math.PI / 2
  alertRing.position.set(0, -1.5, 0)
  scene.add(alertRing)
  updateStateVisuals()

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
    const loadBoost = 1 + Math.min(2.2, (props.queues.inbox + props.queues.outbox + props.activeCount) * 0.15)

    animatedTowers.forEach((tower, i) => {
      tower.rotation.y += dt * (0.28 + i * 0.1)
    })
    for (const p of pulses) {
      const progress = (t * p.speed * loadBoost + p.offset) % 1
      p.curve.getPointAt(progress, p.mesh.position)
    }
    if (alertRing?.visible) {
      const s = 1 + Math.sin(t * 5) * 0.08
      alertRing.scale.setScalar(s)
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
  for (const item of disposables) item.dispose()
  disposables.length = 0
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
.infra-canvas {
  position: relative;
  width: 100%;
  height: 270px;
  border: 1px solid rgba(56, 189, 248, 0.22);
  border-radius: var(--radius);
  overflow: hidden;
  background: radial-gradient(circle at 50% 40%, #091732 0%, #050914 78%);
  cursor: grab;
}
.infra-canvas :deep(canvas) {
  display: block;
  width: 100% !important;
  height: 100% !important;
}
.infra-labels {
  position: absolute;
  inset: 0;
  pointer-events: none;
  overflow: hidden;
}
.infra-tag {
  position: absolute;
  left: 0;
  top: 0;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 3px 10px;
  border: 1px solid rgba(148, 163, 184, 0.28);
  border-radius: 999px;
  background: rgba(7, 13, 27, 0.88);
  color: #f1f5f9;
  font-size: 11px;
  white-space: nowrap;
  backdrop-filter: blur(8px);
}
.infra-tag i {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #38bdf8;
}
.infra-tag span {
  padding-left: 6px;
  border-left: 1px solid rgba(148, 163, 184, 0.25);
  color: #7dd3fc;
  font-variant-numeric: tabular-nums;
}
.infra-tag.green i { background: #34d399; }
.infra-tag.green span { color: #6ee7b7; }
.infra-tag.purple i { background: #818cf8; }
.infra-tag.purple span { color: #c7d2fe; }
.infra-tag.warn i { background: #fbbf24; }
.infra-tag.warn span { color: #fde68a; }
.infra-tag.danger i { background: #f87171; }
.infra-tag.danger span { color: #fca5a5; }
</style>
