/** LAN companion controller: discover/pair/control agents (e.g. ClashFest on a
 *  TV). Live discovery updates arrive via the "companion:agents" Wails event. */
export {
  CompanionDiscover,
  CompanionListAgents,
  CompanionPairByString,
  CompanionPairByPin,
  CompanionStatus,
  CompanionPower,
  CompanionShareSubscription,
  CompanionRename,
  CompanionUnpair,
  CompanionStartDiscovery,
  CompanionStopDiscovery,
} from '../../wailsjs/go/main/App'
