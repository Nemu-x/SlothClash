import {
  GetDesyncStatus as getStatus,
  RunDesyncDiagnostics as runDiagnostics,
  SetDesyncSettings as setSettings,
} from '../../wailsjs/go/main/App'

export interface DesyncSettings {
  enabled: boolean
  youtube: boolean
  discord: boolean
  discordTTL: number
}

export interface DesyncStatus {
  settings: DesyncSettings
  platform: string
  coreSupported: boolean
  connected: boolean
  mode: string
}

export interface DesyncProbeResult {
  name: string
  url: string
  route: string
  ok: boolean
  delayMS: number
  error?: string
}

export const GetDesyncStatus = (): Promise<DesyncStatus> => getStatus()
export const SetDesyncSettings = (
  settings: DesyncSettings,
): Promise<DesyncStatus> => setSettings(settings)
export const RunDesyncDiagnostics = (): Promise<DesyncProbeResult[]> =>
  runDiagnostics()
