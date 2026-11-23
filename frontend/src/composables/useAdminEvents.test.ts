import { mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { expect, it, vi } from 'vitest'
import { useAdminEvents } from './useAdminEvents'

it('shares a socket, merges commits, refreshes after reconnect and stops after unmount', async () => {
  vi.useFakeTimers()
  const sockets: Socket[] = []
  class Socket {
    onclose?: () => void
    onmessage?: (event: {data: string}) => void
    constructor() { sockets.push(this) }
    close() { this.onclose?.() }
    sendChange(resource: string) { this.onmessage?.({ data: JSON.stringify({ resource }) }) }
  }
  vi.stubGlobal('WebSocket', Socket)
  const messages = vi.fn()
  const members = vi.fn()
  const page = mount(defineComponent({ setup() {
    useAdminEvents(['messages'], messages)
    useAdminEvents(['conversation_members'], members)
    return () => h('div')
  } }))
  try {
    expect(sockets).toHaveLength(1)
    sockets[0].sendChange('messages')
    sockets[0].sendChange('messages')
    await vi.advanceTimersByTimeAsync(550)
    expect(messages).toHaveBeenCalledTimes(1)
    expect(members).not.toHaveBeenCalled()
    sockets[0].close()
    await vi.advanceTimersByTimeAsync(550)
    expect(sockets).toHaveLength(2)
    sockets[1].sendChange('*')
    await vi.advanceTimersByTimeAsync(550)
    expect(messages).toHaveBeenCalledTimes(2)
    expect(members).toHaveBeenCalledTimes(1)
  } finally {
    page.unmount()
    vi.useRealTimers()
    vi.unstubAllGlobals()
  }
})
