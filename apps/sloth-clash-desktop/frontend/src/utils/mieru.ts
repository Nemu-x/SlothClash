import {
  decodeAndTrim,
  parseBoolOrPresence,
  parsePortOrDefault,
  parseQueryStringNormalized,
  parseUrlLike,
  safeDecodeURIComponent,
  splitOnce,
  stripUriScheme,
} from './helpers'

export function URI_MIERU(line: string): IProxyMieruConfig {
  const afterScheme = stripUriScheme(
    line,
    ['mieru', 'mierus'],
    'Invalid mieru uri',
  )
  if (!afterScheme) {
    throw new Error('Invalid mieru uri')
  }

  const {
    auth: authRaw,
    host: server,
    port,
    query: addons,
    fragment: nameRaw,
  } = parseUrlLike(afterScheme, { errorMessage: 'Invalid mieru uri' })
  const portNum = parsePortOrDefault(port, 443)

  const auth = safeDecodeURIComponent(authRaw) ?? authRaw
  const decodedName = decodeAndTrim(nameRaw)
  const name = decodedName ?? `MIERU ${server}:${portNum}`
  const proxy: IProxyMieruConfig = {
    type: 'mieru',
    name,
    server,
    port: portNum,
  }

  if (auth) {
    const [username, password] = splitOnce(auth, ':')
    proxy.username = username
    proxy.password = password
  }

  const params = parseQueryStringNormalized(addons)
  for (const [key, value] of Object.entries(params)) {
    switch (key) {
      case 'port-range':
        proxy['port-range'] = value
        break
      case 'transport':
        proxy.transport = value as MieruTransport
        break
      case 'udp':
        proxy.udp = parseBoolOrPresence(value)
        break
      case 'handshake-mode':
        proxy['handshake-mode'] = value
        break
      case 'multiplexing':
        proxy.multiplexing = value as MieruMultiplexing
        break
      default:
        break
    }
  }

  return proxy
}
