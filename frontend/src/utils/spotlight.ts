import { onBeforeUnmount, onMounted } from 'vue'

const SPOT_SELECTOR = '.panel, .dash-card, .entity-row, .stat-strip, .login-card, .kb-atlas, .settings-panel, .panel-surface'

export function useSpotlight(): void {
  let activeEl: HTMLElement | null = null
  let rafId = 0
  let pendingEvent: PointerEvent | null = null

  function clearSpot() {
    if (activeEl) {
      activeEl.removeAttribute('data-spot')
      activeEl = null
    }
  }

  function updateSpot(event: PointerEvent) {
    rafId = 0
    if (event.pointerType && event.pointerType !== 'mouse') {
      clearSpot()
      return
    }
    const target = event.target instanceof Element ? event.target.closest<HTMLElement>(SPOT_SELECTOR) : null
    if (!target) {
      clearSpot()
      return
    }
    if (activeEl && activeEl !== target) {
      activeEl.removeAttribute('data-spot')
    }
    const rect = target.getBoundingClientRect()
    target.style.setProperty('--mx', `${Math.round(event.clientX - rect.left)}px`)
    target.style.setProperty('--my', `${Math.round(event.clientY - rect.top)}px`)
    target.setAttribute('data-spot', '1')
    activeEl = target
  }

  function onPointerMove(event: PointerEvent) {
    pendingEvent = event
    if (!rafId) {
      rafId = window.requestAnimationFrame(() => {
        if (pendingEvent) updateSpot(pendingEvent)
      })
    }
  }

  onMounted(() => {
    if (typeof window === 'undefined') return
    if (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) return
    window.addEventListener('pointermove', onPointerMove, { passive: true })
    window.addEventListener('pointerleave', clearSpot)
    window.addEventListener('blur', clearSpot)
  })

  onBeforeUnmount(() => {
    if (typeof window === 'undefined') return
    window.removeEventListener('pointermove', onPointerMove)
    window.removeEventListener('pointerleave', clearSpot)
    window.removeEventListener('blur', clearSpot)
    if (rafId) window.cancelAnimationFrame(rafId)
    clearSpot()
  })
}
