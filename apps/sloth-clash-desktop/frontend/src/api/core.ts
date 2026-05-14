/**
 * Core lifecycle: Connect / Disconnect, mode switching (rule/global/direct),
 * traffic mode (proxy/tun), TUN bring-up, traffic-tuning and TUN settings push.
 */
export {
  Connect,
  Disconnect,
  EnsureTunReady,
  GetTunStatus,
  SetMode,
  SetTrafficMode,
  SetTrafficSettings,
  SetTunSettings,
} from '../../wailsjs/go/main/App'
