export type Screen =
  | 'home'
  | 'proxies'
  | 'profiles'
  | 'connections'
  | 'rules'
  | 'logs'
  | 'advanced'
  | 'settings'

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
}
