import axios, { AxiosError, type AxiosRequestConfig } from 'axios'
import type { ApiEnvelope, ApiErrorEnvelope } from '@/types'

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
    return response.data.data
  } catch (error) {
    if (error instanceof ApiError) throw error
    const axiosError = error as AxiosError<ApiErrorEnvelope>
    const backendUnavailable = !axiosError.response || axiosError.code === 'ECONNABORTED' || (axiosError.response.status >= 500)
    if (hasFallback && backendUnavailable && mockFallbackEnabled) return structuredClone(fallback) as T
    const apiError = axiosError.response?.data?.error
    throw new ApiError(apiError?.code || 'NETWORK_ERROR', apiError?.message || '暂时无法连接到服务', axiosError.response?.status)
  }
}

export default client
