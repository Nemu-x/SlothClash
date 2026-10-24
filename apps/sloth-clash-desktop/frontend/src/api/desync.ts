import {
  GetDesyncStatus as getStatus,
  GetDesyncUDPStatus as getUDPStatus,
  ReconnectDiscordUDP as reconnectUDP,
  RunDesyncDiagnostics as runDiagnostics,
  SetDesyncSettings as setSettings,
} from '../../wailsjs/go/main/App'

export interface DesyncSettings {
  enabled: boolean
  youtube: boolean
  discord: boolean
  discordTTL: number
  discordUDP: boolean
  discordUDPTTL: number
  discordUDPRepeats: number
}

export interface DesyncStatus {
  settings: DesyncSettings
  platform: string
  coreSupported: boolean
  udpCoreSupported: boolean
  connected: boolean
  mode: string
  traffic: string
}

export interface DesyncUDPStatus {
  directFlows: number
  profileFlows: number
  profileRoutes: string[]
  upload: number
  download: number
  countersAvailable: boolean
  counters: {
    packets: number
    fakePackets: number
    fakeErrors: number
    peers: number
  }
  lastError?: string
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
export const GetDesyncUDPStatus = (): Promise<DesyncUDPStatus> => getUDPStatus()
export const ReconnectDiscordUDP = (): Promise<number> => reconnectUDP()
