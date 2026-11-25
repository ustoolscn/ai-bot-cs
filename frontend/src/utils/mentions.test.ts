import { describe, expect, it } from 'vitest'
import { formatMentions } from './mentions'

describe('formatMentions', () => {
  it('replaces openid mentions with nicknames', () => {
    expect(formatMentions('<@ABC123> 你好 <@!x_y-1>', { ABC123: '小林', 'x_y-1': '阿杰' })).toBe('@小林 你好 @阿杰')
  })
  it('falls back without exposing the openid', () => {
    expect(formatMentions('<@E4F9A0> 在吗')).toBe('@群成员 在吗')
  })
  it('leaves other text alone', () => {
    expect(formatMentions('价格 <5 元 @小林')).toBe('价格 <5 元 @小林')
    expect(formatMentions(undefined)).toBe('')
  })
})
