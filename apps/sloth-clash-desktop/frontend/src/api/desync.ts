import {
  AddDesyncCustomDomain as addCustom,
  AddDesyncService as addService,
  ListDesyncServices as listServices,
  RemoveDesyncCustomDomain as removeCustom,
  SetDesyncCustomDomainEnabled as setCustomEnabled,
  GetDesyncStatus as getStatus,
  GetDesyncAutoStatus as getAutoStatus,
  GetDesyncDiagnosticReport as getReport,
  GetDesyncCapabilities as getCapabilities,
  RetryDesyncAuto as retryAuto,
  ReconnectDiscordUDP as reconnectUDP,
  SetDesyncSettings as setSettings,
  NewDesyncDraft as newDraft,
  DraftDesyncAddCustomDomain as draftAddCustom,
  DraftDesyncAddService as draftAddService,
  AddDesyncRule as addRule,
  UpdateDesyncRule as updateRule,
  DraftDesyncAddRule as draftAddRule,
  DraftDesyncUpdateRule as draftUpdateRule,
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

/** What a user rule matches: a domain and its subdomains, one domain, an app. */
export type DesyncRuleKind = 'suffix' | 'domain' | 'process'
/**
 * Where a user rule sends the traffic: through Desync (a strategy found
 * automatically), the profile's VPN group (not on a Desync-only profile) or
 * directly.
 */
export type DesyncRuleRoute = 'desync' | 'vpn' | 'direct'

/**
 * One entry of the custom list: a user rule ("My rules": `kind`, the
 * `domain` or `process` it matches, `route`) or a catalog service
 * (`service` set, `domain` empty: its domains come from the catalog, see
 * ListDesyncServices; always through Desync).
 */
export interface DesyncCustomEntry {
  id: string
  /** The rule's domain; empty for a process rule and a service. */
  domain: string
  /** The rule's process name (kind process). */
  process?: string
  /** A DesyncRuleKind; absent for a service, filled in for every rule. */
  kind?: string
  /** A DesyncRuleRoute; absent for a service, filled in for every rule. */
  route?: string
  /** Catalog service ID; absent for a rule. */
  service?: string
  enabled: boolean
}

/** A rule as the user typed it; the backend normalizes and validates it. */
export interface DesyncRuleInput {
  kind: DesyncRuleKind
  value: string
  route: DesyncRuleRoute
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
/** AddDesyncRule on a draft (a Desync-only profile's: no VPN route). */
export const DraftDesyncAddRule = (
  draft: DesyncSettings,
  rule: DesyncRuleInput,
): Promise<DesyncSettings> =>
  draftAddRule(
    main.DesyncSettings.createFrom(draft),
    main.DesyncRuleInput.createFrom(rule),
  )
/** UpdateDesyncRule on a draft. */
export const DraftDesyncUpdateRule = (
  draft: DesyncSettings,
  id: string,
  rule: DesyncRuleInput,
): Promise<DesyncSettings> =>
  draftUpdateRule(
    main.DesyncSettings.createFrom(draft),
    id,
    main.DesyncRuleInput.createFrom(rule),
  )
/** AddDesyncService on a draft: validated, nothing saved. */
export const DraftDesyncAddService = (
  draft: DesyncSettings,
  id: string,
): Promise<DesyncSettings> =>
  draftAddService(main.DesyncSettings.createFrom(draft), id)
export const AddDesyncCustomDomain = (domain: string): Promise<DesyncStatus> =>
  addCustom(domain)
/** Add a rule to the active profile, enabled. */
export const AddDesyncRule = (rule: DesyncRuleInput): Promise<DesyncStatus> =>
  addRule(main.DesyncRuleInput.createFrom(rule))
/**
 * Replace rule `id` in place, keeping its on/off state. A changed rule comes
 * back under a new ID.
 */
export const UpdateDesyncRule = (
  id: string,
  rule: DesyncRuleInput,
): Promise<DesyncStatus> =>
  updateRule(id, main.DesyncRuleInput.createFrom(rule))
/** Add catalog service `id` as one enabled entry; it replaces the Desync domain rules it covers. */
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

/**
 * One method of the desync library and whether this build implements it on
 * this OS. Read from the build, not measured on the network.
 */
export interface DesyncCapability {
  /** "tlsrec", "split", "split-wait-sent", "split-wait-acked", "disorder", "oob", "fake-tcp", "fake-udp". */
  method: string
  supported: boolean
  /** Why it is not supported here (the library's English text). */
  reason?: string
  /** Supported, but the moment it waits for is inferred, not read from the system. */
  inferred?: boolean
}

/** One strategy (preset) of the library's catalog. */
export interface DesyncStrategy {
  /** The ID calibration reports in DesyncAutoService.candidate. */
  id: string
  name: string
  /** "tcp" or "udp". */
  protocol: string
  /** Fixed vocabulary the UI translates (desync.strategy.kind.*). */
  kind: string
  /** 1-based number among the strategies of the same kind, catalog order. */
  variant: number
  /** The library's English technical description. */
  description: string
  available: boolean
  reason?: string
  /** Calibration tries it on this computer. */
  tried: boolean
}

export interface DesyncCapabilityReport {
  os: string
  methods: DesyncCapability[]
  strategies: DesyncStrategy[]
}

/** What this build can do on this OS (fixed while the app runs). */
export const GetDesyncCapabilities = (): Promise<DesyncCapabilityReport> =>
  getCapabilities()
