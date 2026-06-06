export type Screen =
  | 'home'
  | 'proxies'
  | 'profiles'
  | 'connections'
  | 'rules'
  | 'logs'
  | 'advanced'
  | 'settings'

/** Canonical Connection.Status values — mirror connection_status.go in Go. */
export type ConnectionStatus =
  | 'disconnected'
  | 'connecting'
  | 'connected'
  | 'reconnecting'
  | 'error'

export const CONN_STATUS = {
  Disconnected: 'disconnected',
  Connecting: 'connecting',
  Connected: 'connected',
  Reconnecting: 'reconnecting',
  Error: 'error',
} as const satisfies Record<string, ConnectionStatus>

/** True while a connect job, established connection, or auto-restart is live. */
export function isConnStatusActive(s: string): boolean {
  return s === 'connecting' || s === 'connected' || s === 'reconnecting'
}

export type ImportModalReason = 'beacon' | 'connect' | 'manual'

export type ConnectionsOverview = {
  reachable?: boolean
  lastError?: string
  uploadTotal?: number
  downloadTotal?: number
  connections?: Array<{
    id: string
    upload?: number
    download?: number
    start?: string
    rule?: string
    rulePayload?: string
    metadata?: {
      host?: string
      destinationIP?: string
      destinationPort?: string
      process?: string
      network?: string
      type?: string
    }
  }>
}

export type CompactSettings = {
  startMinimized: boolean
  launchOnStartup: boolean
  closeToTray: boolean
  dnsSmartFallback: boolean
  dnsIpv6: boolean
  dnsAllowLan: boolean
  logLevel: 'error' | 'warn' | 'info' | 'debug'
  defaultAutoUpdateMinutes: number
  reconnectOnManualProfileUpdate: boolean
  /** UI zoom factor applied via CSS `zoom` on the document root (1 = 100%). */
  uiScale: number
}
