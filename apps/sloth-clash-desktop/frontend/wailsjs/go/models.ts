export namespace main {
  export class AdvancedGeoStatus {
    geoIpPath: string
    geoIpSize: number
    geoIpModified: number
    geoSitePath: string
    geoSiteSize: number
    geoSiteModified: number

    static createFrom(source: any = {}) {
      return new AdvancedGeoStatus(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.geoIpPath = source['geoIpPath']
      this.geoIpSize = source['geoIpSize']
      this.geoIpModified = source['geoIpModified']
      this.geoSitePath = source['geoSitePath']
      this.geoSiteSize = source['geoSiteSize']
      this.geoSiteModified = source['geoSiteModified']
    }
  }
  export class AdvancedPaths {
    dataRoot: string
    runtimeDir: string
    profilesJson: string
    prefsJson: string
    debugLog: string
    serviceLog: string
    geoDir: string
    activeConfig: string

    static createFrom(source: any = {}) {
      return new AdvancedPaths(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.dataRoot = source['dataRoot']
      this.runtimeDir = source['runtimeDir']
      this.profilesJson = source['profilesJson']
      this.prefsJson = source['prefsJson']
      this.debugLog = source['debugLog']
      this.serviceLog = source['serviceLog']
      this.geoDir = source['geoDir']
      this.activeConfig = source['activeConfig']
    }
  }
  export class UIState {
    isLoading: boolean
    activeModal?: string
    activeScreen: string

    static createFrom(source: any = {}) {
      return new UIState(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.isLoading = source['isLoading']
      this.activeModal = source['activeModal']
      this.activeScreen = source['activeScreen']
    }
  }
  export class HomeInsight {
    nodeLatencyMs: number
    latencyError?: string
    exitIp?: string
    exitLine?: string
    exitFlagIso2?: string
    directIp?: string
    directFlagIso2?: string
    directError?: string
    lastError?: string
    uploadKbps: number
    downloadKbps: number
    trafficError?: string
    updatedAt?: number

    static createFrom(source: any = {}) {
      return new HomeInsight(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.nodeLatencyMs = source['nodeLatencyMs']
      this.latencyError = source['latencyError']
      this.exitIp = source['exitIp']
      this.exitLine = source['exitLine']
      this.exitFlagIso2 = source['exitFlagIso2']
      this.directIp = source['directIp']
      this.directFlagIso2 = source['directFlagIso2']
      this.directError = source['directError']
      this.lastError = source['lastError']
      this.uploadKbps = source['uploadKbps']
      this.downloadKbps = source['downloadKbps']
      this.trafficError = source['trafficError']
      this.updatedAt = source['updatedAt']
    }
  }
  export class CoreState {
    running: boolean
    lifecycle?: string
    version?: string
    controllerAddr?: string
    mixedPort?: number
    lastError?: string

    static createFrom(source: any = {}) {
      return new CoreState(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.running = source['running']
      this.lifecycle = source['lifecycle']
      this.version = source['version']
      this.controllerAddr = source['controllerAddr']
      this.mixedPort = source['mixedPort']
      this.lastError = source['lastError']
    }
  }
  export class ServiceState {
    installed: boolean
    running: boolean
    lastError?: string

    static createFrom(source: any = {}) {
      return new ServiceState(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.installed = source['installed']
      this.running = source['running']
      this.lastError = source['lastError']
    }
  }
  export class ProxyGroup {
    name: string
    type: string
    proxies: string[]
    selected?: string

    static createFrom(source: any = {}) {
      return new ProxyGroup(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.name = source['name']
      this.type = source['type']
      this.proxies = source['proxies']
      this.selected = source['selected']
    }
  }
  export class ProxyState {
    groups: ProxyGroup[]
    activeGroup?: string
    lastGoodGroup?: string

    static createFrom(source: any = {}) {
      return new ProxyState(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.groups = this.convertValues(source['groups'], ProxyGroup)
      this.activeGroup = source['activeGroup']
      this.lastGoodGroup = source['lastGoodGroup']
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class Profile {
    id: string
    name: string
    type: string
    url?: string
    subscriptionInfo?: string
    subscriptionSupportUrl?: string
    subscriptionAnnouncement?: string
    lastUpdated?: number
    autoUpdateEnabled?: boolean
    autoUpdateIntervalMinutes?: number
    mergeTemplate?: string
    rulesTemplate?: string
    proxyTemplate?: string
    skipAutoConfig?: boolean
    lastGoodGroup?: string

    static createFrom(source: any = {}) {
      return new Profile(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.id = source['id']
      this.name = source['name']
      this.type = source['type']
      this.url = source['url']
      this.subscriptionInfo = source['subscriptionInfo']
      this.subscriptionSupportUrl = source['subscriptionSupportUrl']
      this.subscriptionAnnouncement = source['subscriptionAnnouncement']
      this.lastUpdated = source['lastUpdated']
      this.autoUpdateEnabled = source['autoUpdateEnabled']
      this.autoUpdateIntervalMinutes = source['autoUpdateIntervalMinutes']
      this.mergeTemplate = source['mergeTemplate']
      this.rulesTemplate = source['rulesTemplate']
      this.proxyTemplate = source['proxyTemplate']
      this.skipAutoConfig = source['skipAutoConfig']
      this.lastGoodGroup = source['lastGoodGroup']
    }
  }
  export class ProfileState {
    activeProfileId?: string
    profiles: Profile[]

    static createFrom(source: any = {}) {
      return new ProfileState(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.activeProfileId = source['activeProfileId']
      this.profiles = this.convertValues(source['profiles'], Profile)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class ModeState {
    current: string
    lastNonDirectMode?: string

    static createFrom(source: any = {}) {
      return new ModeState(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.current = source['current']
      this.lastNonDirectMode = source['lastNonDirectMode']
    }
  }
  export class ConnectionState {
    status: string
    health?: string
    lastError?: string
    lastWarning?: string
    since?: number

    static createFrom(source: any = {}) {
      return new ConnectionState(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.status = source['status']
      this.health = source['health']
      this.lastError = source['lastError']
      this.lastWarning = source['lastWarning']
      this.since = source['since']
    }
  }
  export class AppState {
    connection: ConnectionState
    mode: ModeState
    traffic: string
    profile: ProfileState
    proxy: ProxyState
    service: ServiceState
    core: CoreState
    insight: HomeInsight
    ui: UIState
    updatedAt: number

    static createFrom(source: any = {}) {
      return new AppState(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.connection = this.convertValues(
        source['connection'],
        ConnectionState,
      )
      this.mode = this.convertValues(source['mode'], ModeState)
      this.traffic = source['traffic']
      this.profile = this.convertValues(source['profile'], ProfileState)
      this.proxy = this.convertValues(source['proxy'], ProxyState)
      this.service = this.convertValues(source['service'], ServiceState)
      this.core = this.convertValues(source['core'], CoreState)
      this.insight = this.convertValues(source['insight'], HomeInsight)
      this.ui = this.convertValues(source['ui'], UIState)
      this.updatedAt = source['updatedAt']
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class ConnectionMeta {
    network?: string
    type?: string
    host?: string
    sourceIP?: string
    sourcePort?: string
    destinationIP?: string
    destinationPort?: string
    process?: string

    static createFrom(source: any = {}) {
      return new ConnectionMeta(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.network = source['network']
      this.type = source['type']
      this.host = source['host']
      this.sourceIP = source['sourceIP']
      this.sourcePort = source['sourcePort']
      this.destinationIP = source['destinationIP']
      this.destinationPort = source['destinationPort']
      this.process = source['process']
    }
  }
  export class ConnectionItem {
    id: string
    metadata: ConnectionMeta
    upload: number
    download: number
    start?: string
    rule?: string
    rulePayload?: string
    chains?: string[]

    static createFrom(source: any = {}) {
      return new ConnectionItem(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.id = source['id']
      this.metadata = this.convertValues(source['metadata'], ConnectionMeta)
      this.upload = source['upload']
      this.download = source['download']
      this.start = source['start']
      this.rule = source['rule']
      this.rulePayload = source['rulePayload']
      this.chains = source['chains']
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }

  export class ConnectionsOverview {
    controller: string
    reachable: boolean
    lastError?: string
    uploadTotal: number
    downloadTotal: number
    connections: ConnectionItem[]

    static createFrom(source: any = {}) {
      return new ConnectionsOverview(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.controller = source['controller']
      this.reachable = source['reachable']
      this.lastError = source['lastError']
      this.uploadTotal = source['uploadTotal']
      this.downloadTotal = source['downloadTotal']
      this.connections = this.convertValues(
        source['connections'],
        ConnectionItem,
      )
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }

  export class PrivacySettings {
    hwidEnabled?: boolean

    static createFrom(source: any = {}) {
      return new PrivacySettings(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.hwidEnabled = source['hwidEnabled']
    }
  }
  export class TrafficSettings {
    snifferEnabled?: boolean
    findProcessMode?: string

    static createFrom(source: any = {}) {
      return new TrafficSettings(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.snifferEnabled = source['snifferEnabled']
      this.findProcessMode = source['findProcessMode']
    }
  }
  export class TunSettings {
    stack?: string
    autoRoute?: boolean
    autoDetectInterface?: boolean
    strictRoute?: boolean
    dnsHijack?: string[]
    mtu?: number
    device?: string

    static createFrom(source: any = {}) {
      return new TunSettings(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.stack = source['stack']
      this.autoRoute = source['autoRoute']
      this.autoDetectInterface = source['autoDetectInterface']
      this.strictRoute = source['strictRoute']
      this.dnsHijack = source['dnsHijack']
      this.mtu = source['mtu']
      this.device = source['device']
    }
  }
  export class DesktopPrefs {
    tun: TunSettings
    traffic: TrafficSettings
    privacy: PrivacySettings
    lang?: string

    static createFrom(source: any = {}) {
      return new DesktopPrefs(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.tun = this.convertValues(source['tun'], TunSettings)
      this.traffic = this.convertValues(source['traffic'], TrafficSettings)
      this.privacy = this.convertValues(source['privacy'], PrivacySettings)
      this.lang = source['lang']
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }

  export class ProfileConfigPeek {
    path: string
    body: string
    lastError?: string

    static createFrom(source: any = {}) {
      return new ProfileConfigPeek(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.path = source['path']
      this.body = source['body']
      this.lastError = source['lastError']
    }
  }
  export class ProfilePaths {
    dataDir: string
    configPath: string

    static createFrom(source: any = {}) {
      return new ProfilePaths(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.dataDir = source['dataDir']
      this.configPath = source['configPath']
    }
  }
  export class ProfileProxyGroupsBaseline {
    groups: any[]
    isFullProfile: boolean
    lastError?: string

    static createFrom(source: any = {}) {
      return new ProfileProxyGroupsBaseline(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.groups = source['groups']
      this.isFullProfile = source['isFullProfile']
      this.lastError = source['lastError']
    }
  }
  export class ProfileRulesBaseline {
    rules: string[]
    isFullProfile: boolean
    lastError?: string

    static createFrom(source: any = {}) {
      return new ProfileRulesBaseline(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.rules = source['rules']
      this.isFullProfile = source['isFullProfile']
      this.lastError = source['lastError']
    }
  }

  export class RulesOverview {
    controller: string
    reachable: boolean
    lastError?: string
    rulesBody?: string
    ruleProvidersBody?: string

    static createFrom(source: any = {}) {
      return new RulesOverview(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.controller = source['controller']
      this.reachable = source['reachable']
      this.lastError = source['lastError']
      this.rulesBody = source['rulesBody']
      this.ruleProvidersBody = source['ruleProvidersBody']
    }
  }
  export class RuntimeDiagEvent {
    ts: number
    category: string
    message?: string

    static createFrom(source: any = {}) {
      return new RuntimeDiagEvent(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.ts = source['ts']
      this.category = source['category']
      this.message = source['message']
    }
  }
  export class ServiceLogPeek {
    path: string
    text: string
    truncated: boolean
    lastError?: string

    static createFrom(source: any = {}) {
      return new ServiceLogPeek(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.path = source['path']
      this.text = source['text']
      this.truncated = source['truncated']
      this.lastError = source['lastError']
    }
  }

  export class SubscriptionDeviceIdentityPublic {
    hwid: string
    deviceOs: string
    osVersion: string
    deviceModel: string
    appVersion: string

    static createFrom(source: any = {}) {
      return new SubscriptionDeviceIdentityPublic(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.hwid = source['hwid']
      this.deviceOs = source['deviceOs']
      this.osVersion = source['osVersion']
      this.deviceModel = source['deviceModel']
      this.appVersion = source['appVersion']
    }
  }
  export class SubscriptionPeek {
    url: string
    suggestedName: string
    profileTitleRaw?: string
    httpStatus?: number
    lastError?: string
    subscriptionInfo?: string
    subscriptionSupportUrl?: string
    subscriptionAnnouncement?: string

    static createFrom(source: any = {}) {
      return new SubscriptionPeek(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.url = source['url']
      this.suggestedName = source['suggestedName']
      this.profileTitleRaw = source['profileTitleRaw']
      this.httpStatus = source['httpStatus']
      this.lastError = source['lastError']
      this.subscriptionInfo = source['subscriptionInfo']
      this.subscriptionSupportUrl = source['subscriptionSupportUrl']
      this.subscriptionAnnouncement = source['subscriptionAnnouncement']
    }
  }

  export class TunSetupResult {
    success: boolean
    message: string
    installAction: boolean

    static createFrom(source: any = {}) {
      return new TunSetupResult(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.success = source['success']
      this.message = source['message']
      this.installAction = source['installAction']
    }
  }

  export class UpdateState {
    channel: string
    hasUpdate: boolean
    lastCheckedAt?: number
    currentVersion?: string
    latestVersion?: string
    releaseUrl?: string
    assetName?: string
    assetDownloadUrl?: string
    lastError?: string

    static createFrom(source: any = {}) {
      return new UpdateState(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.channel = source['channel']
      this.hasUpdate = source['hasUpdate']
      this.lastCheckedAt = source['lastCheckedAt']
      this.currentVersion = source['currentVersion']
      this.latestVersion = source['latestVersion']
      this.releaseUrl = source['releaseUrl']
      this.assetName = source['assetName']
      this.assetDownloadUrl = source['assetDownloadUrl']
      this.lastError = source['lastError']
    }
  }
}

export namespace options {
  export class SecondInstanceData {
    Args: string[]
    WorkingDirectory: string

    static createFrom(source: any = {}) {
      return new SecondInstanceData(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.Args = source['Args']
      this.WorkingDirectory = source['WorkingDirectory']
    }
  }
}
