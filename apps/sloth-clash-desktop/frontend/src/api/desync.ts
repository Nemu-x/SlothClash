import {
  AddDesyncCustomDomain as addCustom,
  AddDesyncService as addService,
  ListDesyncServices as listServices,
  RemoveDesyncCustomDomain as removeCustom,
  SetDesyncCustomDomainEnabled as setCustomEnabled,
  GetDesyncStatus as getStatus,
  GetDesyncAutoStatus as getAutoStatus,
  GetDesyncDiagnosticReport as getReport,
  RetryDesyncAuto as retryAuto,
  ReconnectDiscordUDP as reconnectUDP,
  SetDesyncSettings as setSettings,
  NewDesyncDraft as newDraft,
  DraftDesyncAddCustomDomain as draftAddCustom,
  DraftDesyncAddService as draftAddService,
} from '../../wailsjs/go/main/App'

import { main } from './models'

export interface DesyncSettings {
  enabled: boolean
  youtube: boolean
  discord: boolean
  discordUDP: boolean
  /** Custom domains; absent when there are none. */
  custom?: DesyncCustomEntry[]
}

/**
 * One custom entry routed through Desync: a domain (its subdomains included)
 * or a catalog service (`service` set, `domain` empty: its domains come from the
 * catalog, see ListDesyncServices).
 */
export interface DesyncCustomEntry {
  id: string
  domain: string
  /** Catalog service ID; absent for a plain domain entry. */
  service?: string
  enabled: boolean
}

/** One catalog service: a service whose pages and media span several domains. */
export interface DesyncService {
  id: string
  name: string
  /** Every domain the service routes (each with its subdomains). */
  domains: string[]
}

export interface DesyncStatus {
  settings: DesyncSettings
  platform: string
  coreSupported: boolean
  udpCoreSupported: boolean
  connected: boolean
  mode: string
  traffic: string
  /**
   * The active profile is Desync-only: no subscription reserve, a route
   * without a working strategy goes direct (phase no_strategy).
   */
  desyncOnly?: boolean
}

export const GetDesyncStatus = (): Promise<DesyncStatus> => getStatus()
export const SetDesyncSettings = (
  settings: DesyncSettings,
): Promise<DesyncStatus> =>
  setSettings(main.DesyncSettings.createFrom(settings))
/** The selection a new Desync-only profile starts from (the defaults). */
export const NewDesyncDraft = (): Promise<DesyncSettings> => newDraft()
/** AddDesyncCustomDomain on a draft: validated, nothing saved. */
export const DraftDesyncAddCustomDomain = (
  draft: DesyncSettings,
  domain: string,
): Promise<DesyncSettings> =>
  draftAddCustom(main.DesyncSettings.createFrom(draft), domain)
/** AddDesyncService on a draft: validated, nothing saved. */
export const DraftDesyncAddService = (
  draft: DesyncSettings,
  id: string,
): Promise<DesyncSettings> =>
  draftAddService(main.DesyncSettings.createFrom(draft), id)
export const AddDesyncCustomDomain = (domain: string): Promise<DesyncStatus> =>
  addCustom(domain)
/** Add catalog service `id` as one enabled entry; it replaces plain entries it covers. */
export const AddDesyncService = (id: string): Promise<DesyncStatus> =>
  addService(id)
/** The service catalog, in precedence order (fixed per build). */
export const ListDesyncServices = (): Promise<DesyncService[]> => listServices()
export const RemoveDesyncCustomDomain = (id: string): Promise<DesyncStatus> =>
  removeCustom(id)
export const SetDesyncCustomDomainEnabled = (
  id: string,
  enabled: boolean,
): Promise<DesyncStatus> => setCustomEnabled(id, enabled)
/** Close Discord's UDP flows so the client reconnects (the "Reconnect voice" button). */
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
