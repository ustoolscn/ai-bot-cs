export type TreemapItem = { id: string; weight: number }
export type TreemapRect<T extends TreemapItem> = T & { x: number; y: number; w: number; h: number }

interface AreaNode<T extends TreemapItem> {
  item: T
  area: number
}

/**
 * 使用 Squarified Treemap 算法在 [0, width] x [0, height] 内布局矩形。
 * 保证所有正面积项不越界、总面积与容器面积一致。
 */
export function layoutTreemap<T extends TreemapItem>(items: T[], width: number, height: number): Array<TreemapRect<T>> {
  if (!items.length || width <= 0 || height <= 0 || !Number.isFinite(width) || !Number.isFinite(height)) {
    return []
  }

  const normalized = items.map(item => ({
    item,
    weight: Number.isFinite(item.weight) && item.weight > 0 ? item.weight : 1,
  }))
  const totalWeight = normalized.reduce((sum, n) => sum + n.weight, 0)
  if (totalWeight <= 0) return []

  const totalArea = width * height
  const nodes: AreaNode<T>[] = normalized
    .map(n => ({ item: n.item, area: (n.weight / totalWeight) * totalArea }))
    .sort((a, b) => b.area - a.area)

  const rects: Array<TreemapRect<T>> = []
  squarify(nodes, [], { x: 0, y: 0, w: width, h: height }, rects)
  return rects
}

function worstAspectRatio(row: AreaNode<any>[], shortSide: number): number {
  if (!row.length || shortSide <= 0) return Infinity
  const sum = row.reduce((acc, n) => acc + n.area, 0)
  if (sum <= 0) return Infinity
  let maxA = 0
  let minA = Infinity
  for (const n of row) {
    if (n.area > maxA) maxA = n.area
    if (n.area < minA) minA = n.area
  }
  const s2 = shortSide * shortSide
  const sum2 = sum * sum
  return Math.max((s2 * maxA) / sum2, sum2 / (s2 * Math.max(minA, 1e-9)))
}

function layoutRow<T extends TreemapItem>(
  row: AreaNode<T>[],
  box: { x: number; y: number; w: number; h: number },
  out: Array<TreemapRect<T>>,
): { x: number; y: number; w: number; h: number } {
  const rowArea = row.reduce((acc, n) => acc + n.area, 0)
  if (rowArea <= 0 || box.w <= 0 || box.h <= 0) return box

  if (box.w >= box.h) {
    // 容器偏宽：沿左侧竖排一列，占宽度 rowWidth
    const rowWidth = Math.min(box.w, rowArea / box.h)
    let cy = box.y
    for (let i = 0; i < row.length; i++) {
      const n = row[i]
      const itemH = i === row.length - 1 ? Math.max(0, box.y + box.h - cy) : (n.area / rowArea) * box.h
      out.push({ ...n.item, x: box.x, y: cy, w: rowWidth, h: itemH })
      cy += itemH
    }
    return { x: box.x + rowWidth, y: box.y, w: Math.max(0, box.w - rowWidth), h: box.h }
  }

  // 容器偏高：沿顶部横排一行，占高度 rowHeight
  const rowHeight = Math.min(box.h, rowArea / box.w)
  let cx = box.x
  for (let i = 0; i < row.length; i++) {
    const n = row[i]
    const itemW = i === row.length - 1 ? Math.max(0, box.x + box.w - cx) : (n.area / rowArea) * box.w
    out.push({ ...n.item, x: cx, y: box.y, w: itemW, h: rowHeight })
    cx += itemW
  }
  return { x: box.x, y: box.y + rowHeight, w: box.w, h: Math.max(0, box.h - rowHeight) }
}

function squarify<T extends TreemapItem>(
  remaining: AreaNode<T>[],
  row: AreaNode<T>[],
  box: { x: number; y: number; w: number; h: number },
  out: Array<TreemapRect<T>>,
): void {
  if (!remaining.length) {
    if (row.length) layoutRow(row, box, out)
    return
  }

  const shortSide = Math.min(box.w, box.h)
  const head = remaining[0]
  if (!row.length) {
    squarify(remaining.slice(1), [head], box, out)
    return
  }

  const currentWorst = worstAspectRatio(row, shortSide)
  const nextWorst = worstAspectRatio([...row, head], shortSide)
  if (nextWorst <= currentWorst) {
    squarify(remaining.slice(1), [...row, head], box, out)
  } else {
    const nextBox = layoutRow(row, box, out)
    squarify(remaining, [], nextBox, out)
  }
}
