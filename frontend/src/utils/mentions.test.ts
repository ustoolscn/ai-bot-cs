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

describe('QQ faces', () => {
  const ext = (text: string) => btoa(String.fromCharCode(...new TextEncoder().encode(JSON.stringify({ text }))))
  it('shows the face name from ext', () => {
    expect(formatMentions(`哈哈<faceType=1,faceId="13",ext="${ext('[呲牙]')}">`)).toBe('哈哈[呲牙]')
    expect(formatMentions(`<faceType=1,faceId="14",ext="${ext('微笑')}">`)).toBe('[微笑]')
  })
  it('shows unicode emoji and hides unknown codes', () => {
    expect(formatMentions('<faceType=2,faceId="128512",ext="">好')).toBe('😀好')
    expect(formatMentions('<faceType=1,faceId="999",ext="bad">')).toBe('[表情]')
    expect(formatMentions('<emoji:128077>')).toBe('👍')
  })
})
