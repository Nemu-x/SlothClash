import {
  AddDesyncCustomDomain as addCustom,
  RemoveDesyncCustomDomain as removeCustom,
  SetDesyncCustomDomainEnabled as setCustomEnabled,
  GetDesyncStatus as getStatus,
  GetDesyncAutoStatus as getAutoStatus,
  GetDesyncDiagnosticReport as getReport,
  RetryDesyncAuto as retryAuto,
  GetDesyncUDPStatus as getUDPStatus,
  ReconnectDiscordUDP as reconnectUDP,
  RunDesyncDiagnostics as runDiagnostics,
  SetDesyncSettings as setSettings,
} from '../../wailsjs/go/main/App'

import { main } from './models'

export interface DesyncSettings {
  enabled: boolean
  youtube: boolean
  discord: boolean
  discordTTL: number
  discordUDP: boolean
  discordUDPTTL: number
  discordUDPRepeats: number
  /** Custom domains; absent when there are none. */
  custom?: DesyncCustomEntry[]
}

/** One custom domain routed through Desync (its subdomains included). */
export interface DesyncCustomEntry {
  id: string
  domain: string
  enabled: boolean
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
): Promise<DesyncStatus> =>
  setSettings(main.DesyncSettings.createFrom(settings))
export const AddDesyncCustomDomain = (domain: string): Promise<DesyncStatus> =>
  addCustom(domain)
export const RemoveDesyncCustomDomain = (id: string): Promise<DesyncStatus> =>
  removeCustom(id)
export const SetDesyncCustomDomainEnabled = (
  id: string,
  enabled: boolean,
): Promise<DesyncStatus> => setCustomEnabled(id, enabled)
export const RunDesyncDiagnostics = (): Promise<DesyncProbeResult[]> =>
  runDiagnostics()
export const GetDesyncUDPStatus = (): Promise<DesyncUDPStatus> => getUDPStatus()
export const ReconnectDiscordUDP = (): Promise<number> => reconnectUDP()

export interface DesyncAutoService {
  route?: string
  phase: string
  candidate?: string
  attempt: number
  total: number
  code?: string
  updated: number
}
export interface DesyncAutoStatus {
  running: boolean
  network?: string
  youtube: DesyncAutoService
  discord: DesyncAutoService
  voice: DesyncAutoService
  /** Calibration state of each enabled custom entry, by entry id. */
  custom?: Record<string, DesyncAutoService>
}
export const GetDesyncAutoStatus = (): Promise<DesyncAutoStatus> =>
  getAutoStatus()
export const RetryDesyncAuto = (): Promise<DesyncAutoStatus> => retryAuto()
export const GetDesyncDiagnosticReport = (): Promise<string> => getReport()
