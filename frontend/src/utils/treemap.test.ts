import { describe, expect, it } from 'vitest'
import { layoutTreemap } from './treemap'

describe('squarified treemap layout', () => {
  it('returns empty array for empty input or non-positive container dimensions', () => {
    expect(layoutTreemap([], 600, 400)).toEqual([])
    expect(layoutTreemap([{ id: 'a', weight: 10 }], 0, 400)).toEqual([])
    expect(layoutTreemap([{ id: 'a', weight: 10 }], 600, -10)).toEqual([])
  })

  it('fills the entire container for a single item', () => {
    const rects = layoutTreemap([{ id: 'only', weight: 5 }], 800, 500)
    expect(rects).toHaveLength(1)
    expect(rects[0]).toMatchObject({ id: 'only', x: 0, y: 0, w: 800, h: 500 })
  })

  it('conserves total area and keeps all rectangles within bounds for balanced and skewed weights', () => {
    const width = 900
    const height = 520
    const items = [
      { id: 'doc-1', weight: 120 },
      { id: 'doc-2', weight: 45 },
      { id: 'doc-3', weight: 18 },
      { id: 'qa-1', weight: 9 },
      { id: 'qa-2', weight: 0 },
      { id: 'qa-3', weight: 2 },
    ]
    const rects = layoutTreemap(items, width, height)
    expect(rects).toHaveLength(items.length)

    let areaSum = 0
    for (const r of rects) {
      expect(r.x).toBeGreaterThanOrEqual(-1e-6)
      expect(r.y).toBeGreaterThanOrEqual(-1e-6)
      expect(r.w).toBeGreaterThan(0)
      expect(r.h).toBeGreaterThan(0)
      expect(r.x + r.w).toBeLessThanOrEqual(width + 1e-6)
      expect(r.y + r.h).toBeLessThanOrEqual(height + 1e-6)
      areaSum += r.w * r.h
    }
    expect(areaSum).toBeCloseTo(width * height, 4)
  })
})
