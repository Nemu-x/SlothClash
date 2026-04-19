export namespace main {
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
    lastUpdated?: number
    autoUpdateEnabled?: boolean
    autoUpdateIntervalMinutes?: number
    mergeTemplate?: string
    rulesTemplate?: string
    proxyTemplate?: string
    skipAutoConfig?: boolean

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
      this.lastUpdated = source['lastUpdated']
      this.autoUpdateEnabled = source['autoUpdateEnabled']
      this.autoUpdateIntervalMinutes = source['autoUpdateIntervalMinutes']
      this.mergeTemplate = source['mergeTemplate']
      this.rulesTemplate = source['rulesTemplate']
      this.proxyTemplate = source['proxyTemplate']
      this.skipAutoConfig = source['skipAutoConfig']
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
    lastError?: string
    lastWarning?: string
    since?: number

    static createFrom(source: any = {}) {
      return new ConnectionState(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.status = source['status']
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

  export class SubscriptionPeek {
    url: string
    suggestedName: string
    profileTitleRaw?: string
    httpStatus?: number
    lastError?: string
    subscriptionInfo?: string

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

    static createFrom(source: any = {}) {
      return new UpdateState(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.channel = source['channel']
      this.hasUpdate = source['hasUpdate']
      this.lastCheckedAt = source['lastCheckedAt']
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
