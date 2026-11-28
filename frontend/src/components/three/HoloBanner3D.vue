<template>
  <div ref="hostRef" class="holo-banner-3d" aria-hidden="true" />
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as THREE from 'three'

const props = withDefaults(defineProps<{
  variant: 'bots' | 'studio' | 'ops' | 'messages' | 'handoff'
  alert?: boolean
}>(), {
  alert: false,
})

const hostRef = ref<HTMLElement>()
let renderer: THREE.WebGLRenderer | null = null
let scene: THREE.Scene | null = null
let camera: THREE.PerspectiveCamera | null = null
let resizeObserver: ResizeObserver | null = null
let rafId = 0

const disposables: Array<{ dispose: () => void }> = []
const mouseTarget = { x: 0, y: 0 }
const mouseCurrent = { x: 0, y: 0 }
let mainGroup: THREE.Group | null = null
let accentMat: THREE.Material | null = null

function track<T extends { dispose: () => void }>(item: T): T {
  disposables.push(item)
  return item
}

function onPointerMove(e: PointerEvent) {
  mouseTarget.x = (e.clientX / Math.max(window.innerWidth, 1) - 0.5) * 2
  mouseTarget.y = (e.clientY / Math.max(window.innerHeight, 1) - 0.5) * 2
}

watch(() => props.alert, (isAlert) => {
  if (accentMat && 'color' in accentMat) {
    (accentMat as THREE.MeshBasicMaterial).color.setHex(isAlert ? 0xfbbf24 : 0x38bdf8)
  }
})

onMounted(() => {
  const host = hostRef.value
  if (!host || typeof window === 'undefined') return
  const w = Math.max(host.clientWidth, 240)
  const h = Math.max(host.clientHeight, 120)

  try {
    renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true, powerPreference: 'high-performance' })
  } catch {
    return
  }
  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2))
  renderer.setSize(w, h)
  host.appendChild(renderer.domElement)

  scene = new THREE.Scene()
  camera = new THREE.PerspectiveCamera(45, w / h, 0.1, 100)
  camera.position.set(0, 0.4, 7.6)

  mainGroup = new THREE.Group()
  mainGroup.position.x = 2.6
  scene.add(mainGroup)

  const updaters: Array<(t: number, dt: number) => void> = []

  if (props.variant === 'bots') {
    const meshes: THREE.Mesh[] = []
    for (let i = 0; i < 4; i++) {
      const geo = track(new THREE.DodecahedronGeometry(0.65 - i * 0.08, 0))
      const mat = track(new THREE.MeshBasicMaterial({
        color: i % 2 === 0 ? 0x38bdf8 : 0x818cf8,
        wireframe: true,
        transparent: true,
        opacity: 0.65,
      }))
      const m = new THREE.Mesh(geo, mat)
      m.position.set((i - 1.5) * 1.45, Math.sin(i * 1.4) * 0.45, (i % 2) * -0.8)
      mainGroup.add(m)
      meshes.push(m)
    }
    const lineGeo = track(new THREE.BufferGeometry().setFromPoints(meshes.map(m => m.position)))
    const lineMat = track(new THREE.LineBasicMaterial({ color: 0x38bdf8, transparent: true, opacity: 0.38 }))
    mainGroup.add(new THREE.Line(lineGeo, lineMat))
    updaters.push((t, dt) => {
      meshes.forEach((m, i) => {
        m.rotation.y += dt * (0.45 + i * 0.12)
        m.rotation.x += dt * 0.25
        m.position.y = Math.sin(t * 1.6 + i * 1.2) * 0.42
      })
    })
  } else if (props.variant === 'studio') {
    const cols = 22
    const rows = 10
    const count = cols * rows
    const arr = new Float32Array(count * 3)
    const geo = track(new THREE.BufferGeometry())
    geo.setAttribute('position', new THREE.BufferAttribute(arr, 3))
    const mat = track(new THREE.PointsMaterial({ color: 0x38bdf8, size: 0.09, transparent: true, opacity: 0.8, blending: THREE.AdditiveBlending }))
    const pts = new THREE.Points(geo, mat)
    pts.rotation.x = 0.45
    mainGroup.add(pts)
    updaters.push((t) => {
      const pos = geo.getAttribute('position') as THREE.BufferAttribute
      let idx = 0
      for (let x = 0; x < cols; x++) {
        for (let z = 0; z < rows; z++) {
          const px = (x - cols * 0.5) * 0.38
          const pz = (z - rows * 0.5) * 0.42
          const py = Math.sin(px * 0.9 + t * 2.0) * 0.42 + Math.cos(pz * 1.1 + t * 1.5) * 0.28
          pos.setXYZ(idx++, px, py, pz)
        }
      }
      pos.needsUpdate = true
    })
  } else if (props.variant === 'ops') {
    const r1Geo = track(new THREE.TorusGeometry(1.75, 0.025, 8, 64))
    const r1Mat = track(new THREE.MeshBasicMaterial({ color: 0x38bdf8, transparent: true, opacity: 0.65, blending: THREE.AdditiveBlending }))
    const r1 = new THREE.Mesh(r1Geo, r1Mat)
    r1.rotation.set(0.8, 0.3, 0)
    mainGroup.add(r1)

    const r2Geo = track(new THREE.TorusGeometry(1.3, 0.02, 8, 6))
    const r2Mat = track(new THREE.MeshBasicMaterial({ color: 0x34d399, transparent: true, opacity: 0.65, blending: THREE.AdditiveBlending }))
    const r2 = new THREE.Mesh(r2Geo, r2Mat)
    r2.rotation.set(-0.6, 0.4, 0)
    mainGroup.add(r2)

    const coreGeo = track(new THREE.OctahedronGeometry(0.65, 0))
    const coreMat = track(new THREE.MeshBasicMaterial({ color: 0x818cf8, wireframe: true, transparent: true, opacity: 0.75 }))
    const core = new THREE.Mesh(coreGeo, coreMat)
    mainGroup.add(core)

    updaters.push((_t, dt) => {
      r1.rotation.z += dt * 0.4
      r2.rotation.z -= dt * 0.55
      core.rotation.y += dt * 0.5
    })
  } else if (props.variant === 'messages') {
    const count = 220
    const arr = new Float32Array(count * 3)
    for (let i = 0; i < count; i++) {
      arr[i * 3] = (Math.random() - 0.5) * 9
      arr[i * 3 + 1] = (Math.random() - 0.5) * 4
      arr[i * 3 + 2] = (Math.random() - 0.5) * 14
    }
    const geo = track(new THREE.BufferGeometry())
    geo.setAttribute('position', new THREE.BufferAttribute(arr, 3))
    const mat = track(new THREE.PointsMaterial({ color: 0x38bdf8, size: 0.095, transparent: true, opacity: 0.85, blending: THREE.AdditiveBlending }))
    const stream = new THREE.Points(geo, mat)
    mainGroup.add(stream)
    updaters.push((_t, dt) => {
      const pos = geo.getAttribute('position') as THREE.BufferAttribute
      for (let i = 0; i < count; i++) {
        let z = pos.getZ(i) + dt * 5.2
        if (z > 6) z = -8
        pos.setZ(i, z)
      }
      pos.needsUpdate = true
    })
  } else {
    // handoff 全息雷达波纹
    const rings: THREE.Mesh[] = []
    for (let i = 0; i < 4; i++) {
      const g = track(new THREE.RingGeometry(0.45 + i * 0.45, 0.5 + i * 0.45, 48))
      const m = track(new THREE.MeshBasicMaterial({
        color: props.alert ? 0xfbbf24 : 0x38bdf8,
        side: THREE.DoubleSide,
        transparent: true,
        opacity: 0.6 - i * 0.1,
        blending: THREE.AdditiveBlending,
      }))
      if (i === 0) accentMat = m
      const mesh = new THREE.Mesh(g, m)
      mesh.rotation.x = 1.05
      mainGroup.add(mesh)
      rings.push(mesh)
    }
    updaters.push((t) => {
      const speed = props.alert ? 3.2 : 1.6
      rings.forEach((r, i) => {
        const s = 0.85 + ((t * speed * 0.35 + i * 0.25) % 1) * 0.65
        r.scale.setScalar(s)
      })
    })
  }

  window.addEventListener('pointermove', onPointerMove, { passive: true })

  if (typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(entries => {
      const rect = entries[0]?.contentRect
      if (!rect || rect.width <= 0 || rect.height <= 0 || !renderer || !camera) return
      const nw = Math.floor(rect.width)
      const nh = Math.floor(rect.height)
      camera.aspect = nw / nh
      camera.updateProjectionMatrix()
      renderer.setSize(nw, nh)
    })
    resizeObserver.observe(host)
  }

  const clock = new THREE.Clock()
  const animate = () => {
    rafId = window.requestAnimationFrame(animate)
    const dt = clock.getDelta()
    const t = clock.elapsedTime
    mouseCurrent.x += (mouseTarget.x - mouseCurrent.x) * 0.06
    mouseCurrent.y += (mouseTarget.y - mouseCurrent.y) * 0.06
    if (mainGroup) {
      mainGroup.rotation.y = mouseCurrent.x * 0.16
      mainGroup.rotation.x = mouseCurrent.y * 0.12
    }
    for (const u of updaters) u(t, dt)
    renderer?.render(scene!, camera!)
  }
  animate()
})

onBeforeUnmount(() => {
  window.removeEventListener('pointermove', onPointerMove)
  if (rafId) window.cancelAnimationFrame(rafId)
  resizeObserver?.disconnect()
  for (const d of disposables) d.dispose()
  disposables.length = 0
  if (renderer) {
    renderer.dispose()
    renderer.domElement.remove()
  }
  renderer = null
  scene = null
  camera = null
})
</script>

<style scoped>
.holo-banner-3d {
  width: 100%;
  height: 100%;
  overflow: hidden;
  pointer-events: none;
}
.holo-banner-3d :deep(canvas) {
  display: block;
  width: 100% !important;
  height: 100% !important;
}
</style>
