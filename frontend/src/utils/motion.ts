import gsap from 'gsap'

/**
 * 统一科技感错峰入场动效；遵循 prefers-reduced-motion 无障碍规范。
 */
export function animateStagger(
  root: HTMLElement | null | undefined,
  selector = '.panel, .entity-row, .settings-card, .dash-card, .holo-kpi',
  stagger = 0.045,
) {
  if (!root || typeof window === 'undefined') return
  if (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) return
  const targets = Array.from(root.querySelectorAll(selector)).slice(0, 24)
  if (!targets.length) return
  gsap.fromTo(
    targets,
    { opacity: 0, y: 14 },
    {
      opacity: 1,
      y: 0,
      duration: 0.4,
      stagger,
      ease: 'power2.out',
      clearProps: 'transform,opacity',
    },
  )
}
