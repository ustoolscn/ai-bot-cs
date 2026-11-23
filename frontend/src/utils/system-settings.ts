import type { SystemSettings } from '@/types'

export function createSystemSettingsForm(): SystemSettings {
  return { publicBaseUrl: '', defaultContextLimit: 20, aiRequestTimeoutSeconds: 90, messageRetentionDays: 90 }
}

export function applySystemSettings(target: SystemSettings, source: SystemSettings) {
  target.publicBaseUrl = source.publicBaseUrl || ''
  target.defaultContextLimit = source.defaultContextLimit
  target.aiRequestTimeoutSeconds = source.aiRequestTimeoutSeconds
  target.messageRetentionDays = source.messageRetentionDays
  target.updatedAt = source.updatedAt
}
