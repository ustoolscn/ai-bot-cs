import axios, { AxiosError, type AxiosRequestConfig } from 'axios'
import type { ApiEnvelope, ApiErrorEnvelope } from '@/types'
import { connection } from '@/utils/connection'

export class ApiError extends Error {
  constructor(public code: string, message: string, public status?: number) { super(message) }
}

const client = axios.create({ baseURL: '/api', timeout: 6000, withCredentials: true })
export const mockFallbackEnabled = import.meta.env.DEV || import.meta.env.VITE_ENABLE_MOCK_FALLBACK === 'true'

export async function request<T>(config: AxiosRequestConfig, fallback?: T): Promise<T> {
  const hasFallback = arguments.length >= 2
  try {
    const response = await client.request<ApiEnvelope<T> | ApiErrorEnvelope>(config)
    if ('error' in response.data) throw new ApiError(response.data.error.code, response.data.error.message, response.status)
    connection.lost = false
    return response.data.data
  } catch (error) {
    if (error instanceof ApiError) throw error
    const axiosError = error as AxiosError<ApiErrorEnvelope>
    const status = axiosError.response?.status
    const backendUnavailable = !axiosError.response || status === 502 || status === 503 || status === 504
    connection.lost = backendUnavailable
    if (hasFallback && backendUnavailable && mockFallbackEnabled) { connection.usingMock = true; return structuredClone(fallback) as T }
    const apiError = axiosError.response?.data?.error
    throw new ApiError(apiError?.code || (backendUnavailable ? 'NETWORK_ERROR' : 'HTTP_ERROR'), apiError?.message || (backendUnavailable ? '暂时无法连接到服务' : `请求失败（HTTP ${status}）`), status)
  }
}

export default client
