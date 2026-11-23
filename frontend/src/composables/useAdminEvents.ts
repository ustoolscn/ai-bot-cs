import { onBeforeUnmount, onMounted, ref } from 'vue'

type Listener = (resource: string) => void
const listeners = new Set<Listener>()
export const reconnecting = ref(false)
let socket: WebSocket | undefined
let retry: number | undefined
let delay = 500

function connect() {
  if (socket || !listeners.size) return
  const url = new URL('/api/events', window.location.href)
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  const ws = new WebSocket(url)
  socket = ws
  ws.onmessage = ({ data }) => {
    if (socket !== ws) return
    const { resource } = JSON.parse(data)
    delay = 500
    reconnecting.value = false
    if (resource) for (const listener of listeners) listener(resource)
  }
  ws.onclose = () => {
    if (socket !== ws) return
    socket = undefined
    reconnecting.value = true
    retry = window.setTimeout(connect, delay)
    delay = Math.min(delay * 2, 15000)
  }
}

// Merge bursts and repeat the read if another commit lands while it is running.
export function useAdminEvents(resources: string[], refresh: () => Promise<unknown> | void) {
  let mounted = false
  let dirty = false
  let busy = false
  let backoff = 500
  let timer: number | undefined
  function schedule() {
    if (!mounted || busy || timer !== undefined) return
    timer = window.setTimeout(async () => {
      timer = undefined
      busy = true
      dirty = false
      try { await refresh(); backoff = 500 }
      catch (error) { console.warn('实时数据读取失败', error); dirty = true; backoff = Math.min(backoff * 2, 15000) }
      finally { busy = false; if (dirty) schedule() }
    }, backoff)
  }
  const listener: Listener = resource => {
    if (resource !== '*' && !resources.includes(resource)) return
    dirty = true
    schedule()
  }
  onMounted(() => {
    mounted = true
    listeners.add(listener)
    connect()
  })
  onBeforeUnmount(() => {
    mounted = false
    window.clearTimeout(timer)
    listeners.delete(listener)
    if (!listeners.size) {
      window.clearTimeout(retry)
      const current = socket
      socket = undefined
      current?.close()
      reconnecting.value = false
      delay = 500
    }
  })
}
