import type {
  DesyncAutoService,
  DesyncAutoStatus,
  DesyncCapabilityReport,
  DesyncService,
  DesyncSettings,
  DesyncStatus,
} from '../api/desync'
import type { DesyncSnapshot } from '../utils/desyncStatus'

export function desyncSettings(
  over: Partial<DesyncSettings> = {},
): DesyncSettings {
  return {
    enabled: true,
    youtube: true,
    discord: true,
    discordUDP: true,
    ...over,
  }
}

export function desyncStatus(
  over: Partial<Omit<DesyncStatus, 'settings'>> & {
    settings?: Partial<DesyncSettings>
  } = {},
): DesyncStatus {
  const { settings, ...rest } = over
  return {
    settings: desyncSettings(settings),
    platform: 'windows',
    coreSupported: true,
    udpCoreSupported: true,
    connected: true,
    mode: 'rule',
    traffic: 'tun',
    ...rest,
  }
}

export function desyncService(
  phase: string,
  over: Partial<DesyncAutoService> = {},
): DesyncAutoService {
  return { phase, attempt: 0, total: 0, updated: 0, ...over }
}

export function desyncAuto(
  over: Partial<DesyncAutoStatus> = {},
): DesyncAutoStatus {
  return {
    running: true,
    youtube: desyncService('ready'),
    discord: desyncService('ready'),
    voice: desyncService('transport'),
    ...over,
  }
}

export function desyncSnapshot(
  status: DesyncStatus = desyncStatus(),
  automatic: DesyncAutoStatus = desyncAuto(),
): DesyncSnapshot {
  return { status, automatic }
}

/** The service catalog as ListDesyncServices returns it (internal/desync/services.go). */
export const DESYNC_SERVICES: DesyncService[] = [
  {
    id: 'facebook',
    name: 'Facebook',
    domains: [
      'facebook.com',
      'fbcdn.net',
      'facebook.net',
      'fbsbx.com',
      'fb.com',
      'fb.me',
      'fb.watch',
      'messenger.com',
      'm.me',
    ],
  },
  {
    id: 'instagram',
    name: 'Instagram',
    domains: [
      'instagram.com',
      'cdninstagram.com',
      'ig.me',
      'instagr.am',
      'fbcdn.net',
    ],
  },
  {
    id: 'x',
    name: 'X (Twitter)',
    domains: ['x.com', 'twitter.com', 'twimg.com', 't.co', 'pscp.tv'],
  },
]

/**
 * A strategy catalog as GetDesyncCapabilities reports it (internal/desync/
 * strategy.go): IDs are made up, kinds and variants follow the catalog rules.
 */
export const DESYNC_TLSREC_2 = 's15f5cfcd23'
export const DESYNC_UDP_ZERO_1 = 'uebdba14699'

export function desyncCapabilities(
  over: Partial<DesyncCapabilityReport> = {},
): DesyncCapabilityReport {
  return {
    os: 'windows',
    methods: [
      { method: 'tlsrec', supported: true },
      { method: 'split', supported: true },
      { method: 'split-wait-sent', supported: true, inferred: true },
      {
        method: 'split-wait-acked',
        supported: false,
        reason: 'acked split-wait needs TCP_INFO',
      },
      { method: 'disorder', supported: true },
      { method: 'oob', supported: true },
      { method: 'fake-tcp', supported: true },
      { method: 'fake-udp', supported: true },
    ],
    strategies: [
      {
        id: 's0000000001',
        name: 'tlsrec-1',
        protocol: 'tcp',
        kind: 'tlsrec',
        variant: 1,
        description: 'TLS record split at 1',
        available: true,
        tried: true,
      },
      {
        id: DESYNC_TLSREC_2,
        name: 'tlsrec-sni',
        protocol: 'tcp',
        kind: 'tlsrec',
        variant: 2,
        description: 'TLS record split at the SNI',
        available: true,
        tried: true,
      },
      {
        id: 's0000000003',
        name: 'split-wait-acked',
        protocol: 'tcp',
        kind: 'split-wait',
        variant: 1,
        description: 'Split, wait for the ACK',
        available: false,
        reason: 'acked split-wait needs TCP_INFO',
        tried: false,
      },
      {
        id: DESYNC_UDP_ZERO_1,
        name: 'udp-zero-1',
        protocol: 'udp',
        kind: 'udp-zero',
        variant: 1,
        description: 'Zero-payload datagrams with a low TTL',
        available: true,
        tried: true,
      },
    ],
    ...over,
  }
}
