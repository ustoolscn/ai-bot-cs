import { onBeforeUnmount, ref, watch, type Ref } from 'vue'

const reduceMotion = () => typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
const easeOut = (t: number) => 1 - Math.pow(1 - t, 3)

/** 数值平滑过渡到新值，用来表达“数量变了多少”。 */
export function useTween(target: Ref<number>, duration = 700) {
  const value = ref(0)
  let frame = 0
  watch(target, to => {
    cancelAnimationFrame(frame)
    if (!Number.isFinite(to)) { value.value = 0; return }
    if (reduceMotion()) { value.value = to; return }
    const from = value.value
    const start = performance.now()
    const step = (now: number) => {
      const t = Math.min(1, (now - start) / duration)
      value.value = from + (to - from) * easeOut(t)
      if (t < 1) frame = requestAnimationFrame(step)
    }
    frame = requestAnimationFrame(step)
  }, { immediate: true })
  onBeforeUnmount(() => cancelAnimationFrame(frame))
  return value
}
