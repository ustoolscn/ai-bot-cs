import { onBeforeUnmount, ref, watch, type Ref } from 'vue'

const reduceMotion = () => typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
export const easeOutCubic = (t: number) => 1 - Math.pow(1 - t, 3)
export const easeOutBack = (t: number) => { const c1 = 1.4; const c3 = c1 + 1; return 1 + c3 * Math.pow(t - 1, 3) + c1 * Math.pow(t - 1, 2) }

/** 把一个数值平滑过渡到目标值（requestAnimationFrame），用于数字滚动、进度条、圆环。 */
export function useTween(target: Ref<number>, options: { duration?: number; delay?: number; easing?: (t: number) => number; from?: number } = {}) {
  const { duration = 900, delay = 0, easing = easeOutCubic } = options
  const value = ref(options.from ?? 0)
  let frame = 0
  let timer: ReturnType<typeof setTimeout> | undefined

  function run(to: number) {
    cancelAnimationFrame(frame)
    clearTimeout(timer)
    if (!Number.isFinite(to)) { value.value = 0; return }
    if (reduceMotion()) { value.value = to; return }
    const from = value.value
    timer = setTimeout(() => {
      const start = performance.now()
      const step = (now: number) => {
        const t = Math.min(1, (now - start) / duration)
        value.value = from + (to - from) * easing(t)
        if (t < 1) frame = requestAnimationFrame(step)
      }
      frame = requestAnimationFrame(step)
    }, delay)
  }

  watch(target, run, { immediate: true })
  onBeforeUnmount(() => { cancelAnimationFrame(frame); clearTimeout(timer) })
  return value
}
