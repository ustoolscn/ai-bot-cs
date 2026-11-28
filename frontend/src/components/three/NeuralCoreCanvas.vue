<template>
  <div ref="hostRef" class="neural-canvas" aria-hidden="true" />
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as THREE from 'three'
import { EffectComposer } from 'three/examples/jsm/postprocessing/EffectComposer.js'
import { RenderPass } from 'three/examples/jsm/postprocessing/RenderPass.js'
import { UnrealBloomPass } from 'three/examples/jsm/postprocessing/UnrealBloomPass.js'
import { OutputPass } from 'three/examples/jsm/postprocessing/OutputPass.js'

const props = withDefaults(defineProps<{
  mode?: 'login' | 'idle' | 'busy'
  interactive?: boolean
}>(), {
  mode: 'idle',
  interactive: true,
})

const hostRef = ref<HTMLElement>()
let rafId = 0
let resizeObserver: ResizeObserver | null = null
let renderer: THREE.WebGLRenderer | null = null
let composer: EffectComposer | null = null
let scene: THREE.Scene | null = null
let camera: THREE.PerspectiveCamera | null = null

let coreGroup: THREE.Group | null = null
let innerCore: THREE.Mesh | null = null
let shellMesh: THREE.Mesh | null = null
let particleMat: THREE.PointsMaterial | null = null
let lineMat: THREE.LineBasicMaterial | null = null
let bloomPass: UnrealBloomPass | null = null

const disposables: Array<{ dispose: () => void }> = []
const mouseTarget = { x: 0, y: 0 }
const mouseCurrent = { x: 0, y: 0 }

function track<T extends { dispose: () => void }>(item: T): T {
  disposables.push(item)
  return item
}

function onPointerMove(e: PointerEvent) {
  if (!props.interactive) return
  mouseTarget.x = (e.clientX / Math.max(window.innerWidth, 1) - 0.5) * 2
  mouseTarget.y = (e.clientY / Math.max(window.innerHeight, 1) - 0.5) * 2
}

function applyModeVisuals() {
  const isBusy = props.mode === 'busy'
  const isLogin = props.mode === 'login'
  if (innerCore) {
    const mat = innerCore.material as THREE.MeshBasicMaterial
    mat.color.setHex(isBusy ? 0x00f5d4 : 0x38bdf8)
  }
  if (shellMesh) {
    const mat = shellMesh.material as THREE.MeshBasicMaterial
    mat.color.setHex(isBusy ? 0xa855f7 : 0x818cf8)
  }
  if (particleMat) {
    particleMat.color.setHex(isBusy ? 0x5eead4 : 0x38bdf8)
    particleMat.opacity = isBusy ? 0.95 : 0.82
  }
  if (lineMat) {
    lineMat.color.setHex(isBusy ? 0x38bdf8 : 0x6366f1)
    lineMat.opacity = isBusy ? 0.34 : 0.18
  }
  if (bloomPass) {
    bloomPass.strength = isBusy ? 1.25 : isLogin ? 0.95 : 0.78
  }
  if (coreGroup) {
    coreGroup.position.x = isLogin ? -2.2 : 0
  }
}

watch(() => props.mode, applyModeVisuals)

onMounted(() => {
  const host = hostRef.value
  if (!host || typeof window === 'undefined') return
  const width = Math.max(host.clientWidth, 120)
  const height = Math.max(host.clientHeight, 120)

  try {
    renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true, powerPreference: 'high-performance' })
  } catch {
    return
  }

  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2))
  renderer.setSize(width, height)
  renderer.toneMapping = THREE.ACESFilmicToneMapping
  renderer.toneMappingExposure = 1.15
  host.appendChild(renderer.domElement)

  scene = new THREE.Scene()
  scene.fog = new THREE.FogExp2(0x040814, props.mode === 'login' ? 0.045 : 0.03)

  camera = new THREE.PerspectiveCamera(52, width / height, 0.1, 100)
  camera.position.set(0, 0.2, props.mode === 'login' ? 8.2 : 6.4)

  composer = new EffectComposer(renderer)
  composer.addPass(new RenderPass(scene, camera))
  bloomPass = new UnrealBloomPass(new THREE.Vector2(width, height), 0.9, 0.45, 0.18)
  composer.addPass(bloomPass)
  composer.addPass(new OutputPass())

  coreGroup = new THREE.Group()
  scene.add(coreGroup)

  // 1. 斐波那契球面神经节点
  const nodeCount = 260
  const sphereRadius = 2.05
  const pts: THREE.Vector3[] = []
  const posArr = new Float32Array(nodeCount * 3)
  const goldenAngle = Math.PI * (3 - Math.sqrt(5))
  for (let i = 0; i < nodeCount; i++) {
    const y = 1 - (i / (nodeCount - 1)) * 2
    const r = Math.sqrt(Math.max(0, 1 - y * y))
    const theta = goldenAngle * i
    const x = Math.cos(theta) * r
    const z = Math.sin(theta) * r
    const v = new THREE.Vector3(x * sphereRadius, y * sphereRadius, z * sphereRadius)
    pts.push(v)
    posArr[i * 3] = v.x
    posArr[i * 3 + 1] = v.y
    posArr[i * 3 + 2] = v.z
  }
  const ptsGeo = track(new THREE.BufferGeometry())
  ptsGeo.setAttribute('position', new THREE.BufferAttribute(posArr, 3))
  particleMat = track(new THREE.PointsMaterial({
    color: 0x38bdf8,
    size: 0.085,
    transparent: true,
    opacity: 0.85,
    blending: THREE.AdditiveBlending,
    depthWrite: false,
  }))
  const pointCloud = new THREE.Points(ptsGeo, particleMat)
  coreGroup.add(pointCloud)

  // 2. 突触连接线网
  const linePositions: number[] = []
  const threshold = 0.62
  for (let i = 0; i < pts.length; i++) {
    for (let j = i + 1; j < pts.length; j++) {
      if (pts[i].distanceTo(pts[j]) < threshold) {
        linePositions.push(pts[i].x, pts[i].y, pts[i].z, pts[j].x, pts[j].y, pts[j].z)
      }
    }
  }
  const lineGeo = track(new THREE.BufferGeometry())
  lineGeo.setAttribute('position', new THREE.Float32BufferAttribute(linePositions, 3))
  lineMat = track(new THREE.LineBasicMaterial({
    color: 0x6366f1,
    transparent: true,
    opacity: 0.2,
    blending: THREE.AdditiveBlending,
    depthWrite: false,
  }))
  coreGroup.add(new THREE.LineSegments(lineGeo, lineMat))

  // 3. 中心多面体能量核
  const innerGeo = track(new THREE.IcosahedronGeometry(0.68, 1))
  const innerMat = track(new THREE.MeshBasicMaterial({
    color: 0x38bdf8,
    wireframe: true,
    transparent: true,
    opacity: 0.55,
  }))
  innerCore = new THREE.Mesh(innerGeo, innerMat)
  coreGroup.add(innerCore)

  const shellGeo = track(new THREE.IcosahedronGeometry(1.18, 1))
  const shellMat = track(new THREE.MeshBasicMaterial({
    color: 0x818cf8,
    wireframe: true,
    transparent: true,
    opacity: 0.22,
  }))
  shellMesh = new THREE.Mesh(shellGeo, shellMat)
  coreGroup.add(shellMesh)

  // 4. 三层倾斜科幻轨道环与脉冲卫星
  const ringConfigs = [
    { radius: 2.55, tube: 0.012, rx: 1.15, ry: 0.35, color: 0x38bdf8, speed: 0.38 },
    { radius: 2.95, tube: 0.01, rx: -0.75, ry: 0.82, color: 0x818cf8, speed: -0.26 },
    { radius: 3.35, tube: 0.008, rx: 0.42, ry: -1.1, color: 0x34d399, speed: 0.19 },
  ]
  const rings: Array<{ group: THREE.Group; speed: number }> = []
  for (const cfg of ringConfigs) {
    const rGroup = new THREE.Group()
    rGroup.rotation.set(cfg.rx, cfg.ry, 0)
    const torusGeo = track(new THREE.TorusGeometry(cfg.radius, cfg.tube, 8, 96))
    const torusMat = track(new THREE.MeshBasicMaterial({
      color: cfg.color,
      transparent: true,
      opacity: 0.38,
      blending: THREE.AdditiveBlending,
    }))
    rGroup.add(new THREE.Mesh(torusGeo, torusMat))

    const satGeo = track(new THREE.SphereGeometry(0.055, 10, 10))
    const satMat = track(new THREE.MeshBasicMaterial({ color: cfg.color }))
    const sat = new THREE.Mesh(satGeo, satMat)
    sat.position.set(cfg.radius, 0, 0)
    rGroup.add(sat)

    coreGroup.add(rGroup)
    rings.push({ group: rGroup, speed: cfg.speed })
  }

  // 5. 远景深空星尘
  const dustCount = props.mode === 'login' ? 1100 : 320
  const dustArr = new Float32Array(dustCount * 3)
  for (let i = 0; i < dustCount; i++) {
    dustArr[i * 3] = (Math.random() - 0.5) * 24
    dustArr[i * 3 + 1] = (Math.random() - 0.5) * 16
    dustArr[i * 3 + 2] = (Math.random() - 0.5) * 18 - 2
  }
  const dustGeo = track(new THREE.BufferGeometry())
  dustGeo.setAttribute('position', new THREE.BufferAttribute(dustArr, 3))
  const dustMat = track(new THREE.PointsMaterial({
    color: 0x94a3b8,
    size: props.mode === 'login' ? 0.045 : 0.035,
    transparent: true,
    opacity: 0.45,
    depthWrite: false,
  }))
  const dustPoints = new THREE.Points(dustGeo, dustMat)
  scene.add(dustPoints)

  applyModeVisuals()
  window.addEventListener('pointermove', onPointerMove, { passive: true })

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
  const animate = () => {
    rafId = window.requestAnimationFrame(animate)
    const dt = clock.getDelta()
    const elapsed = clock.elapsedTime
    const mul = props.mode === 'busy' ? 2.4 : 1.0

    mouseCurrent.x += (mouseTarget.x - mouseCurrent.x) * 0.06
    mouseCurrent.y += (mouseTarget.y - mouseCurrent.y) * 0.06

    if (coreGroup) {
      coreGroup.rotation.y += dt * 0.18 * mul
      coreGroup.rotation.x = mouseCurrent.y * 0.22
      coreGroup.rotation.z = -mouseCurrent.x * 0.12
    }
    if (innerCore) {
      innerCore.rotation.y -= dt * 0.55 * mul
      innerCore.rotation.x += dt * 0.35 * mul
      const pulse = 1 + Math.sin(elapsed * (props.mode === 'busy' ? 4.5 : 2.0)) * (props.mode === 'busy' ? 0.1 : 0.04)
      innerCore.scale.setScalar(pulse)
    }
    if (shellMesh) {
      shellMesh.rotation.y += dt * 0.24 * mul
    }
    for (const r of rings) {
      r.group.rotation.z += dt * r.speed * mul
    }
    dustPoints.rotation.y += dt * 0.025
    composer?.render()
  }
  animate()
})

onBeforeUnmount(() => {
  window.removeEventListener('pointermove', onPointerMove)
  if (rafId) window.cancelAnimationFrame(rafId)
  resizeObserver?.disconnect()
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
})
</script>

<style scoped>
.neural-canvas {
  width: 100%;
  height: 100%;
  overflow: hidden;
  pointer-events: none;
}
.neural-canvas :deep(canvas) {
  display: block;
  width: 100% !important;
  height: 100% !important;
}
</style>
