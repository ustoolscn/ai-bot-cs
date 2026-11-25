// QQ 消息里的 @ 是 <@openid>（或 <@!openid>）。展示时换成 @昵称；查不到昵称就显示 @群成员，不露出 openid。
const MENTION = /<@!?([A-Za-z0-9_-]+)>/g

export function formatMentions(text: string | null | undefined, names?: Record<string, string> | null): string {
  if (!text) return text || ''
  return text.replace(MENTION, (_, id: string) => `@${names?.[id] || '群成员'}`)
}
