/** Corporate-VPN (OpenConnect sidecar) coexistence controls. macOS-only in P1. */
export {
  ForgetCorpVpnCredentials,
  ForgetCorpVpnServercert,
  GetCorpVpnCredentials,
  GetCorpVpnStatus,
  StartCorpVpn,
  StopCorpVpn,
} from '../../wailsjs/go/main/App'
export type { main } from './models'

// The backend encodes structured errors as "<CODE>: <detail>" (corp_vpn_cert.go).
// Known codes get a localized headline; the raw detail stays visible below it.
const CORP_ERROR_CODES = new Set([
  'VPN_AUTH_FAILED',
  'VPN_CONNECT_FAILED',
  'VPN_CONNECT_IN_PROGRESS',
  'INVALID_PIN',
  'SERVICE_UNAVAILABLE',
  'SERVICE_OUTDATED',
  'SERVICE_ERROR',
])

export function splitCorpError(raw: string): { code: string; detail: string } {
  const m = /^([A-Z_]+):\s*([\s\S]*)$/.exec(raw)
  if (m && CORP_ERROR_CODES.has(m[1])) return { code: m[1], detail: m[2] }
  return { code: '', detail: raw }
}
