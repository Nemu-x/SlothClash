import type {
  DesyncAutoService,
  DesyncAutoStatus,
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
    discordTTL: 0,
    discordUDP: true,
    discordUDPTTL: 0,
    discordUDPRepeats: 0,
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
