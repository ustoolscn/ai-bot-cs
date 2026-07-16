import { describe, expect, it } from 'vitest'
import { formatDuration, statusText } from './format'

describe('formatDuration', () => {
  it('formats milliseconds and seconds', () => {
    expect(formatDuration(320)).toBe('320ms')
    expect(formatDuration(1750)).toBe('1.75s')
    expect(formatDuration()).toBe('—')
  })
})

describe('statusText', () => {
  it('maps known states and keeps unknown states', () => {
    expect(statusText('success')).toBe('成功')
    expect(statusText('custom')).toBe('custom')
  })
})
