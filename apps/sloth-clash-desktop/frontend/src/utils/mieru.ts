import {
  decodeAndTrim,
  parseBoolOrPresence,
  parsePortOrDefault,
  parseQueryStringNormalized,
  safeDecodeURIComponent,
  splitOnce,
  stripUriScheme,
} from './helpers'

/** Split `#frag` then `?query` from the authority tail (after `mieru://`). */
function splitMieruQueryAndFragment(afterScheme: string): {
  base: string
  query?: string
  fragment?: string
} {
  let s = afterScheme
  let fragment: string | undefined
  const hi = s.indexOf('#')
  if (hi !== -1) {
    fragment = s.slice(hi + 1)
    s = s.slice(0, hi)
  }
  let query: string | undefined
  const qi = s.indexOf('?')
  if (qi !== -1) {
    query = s.slice(qi + 1)
    s = s.slice(0, qi)
  }
  return { base: s, query, fragment }
}

/**
 * Host may be IPv4, hostname, or bracketed IPv6. Port is the final `:digits`
 * when that suffix is a valid port (same idea as parseUrlLike host tail).
 */
function parseMieruHostPort(tail: string): { host: string; port?: string } {
  const t = tail.trim()
  if (!t) return { host: '' }
  if (t.startsWith('[')) {
    const end = t.indexOf(']')
    if (end !== -1) {
      const host = t.slice(0, end + 1)
      const after = t.slice(end + 1)
      if (after.startsWith(':')) {
        const p = after.slice(1)
        if (/^\d{1,5}$/.test(p)) return { host, port: p }
      }
      return { host }
    }
  }
  const lastColon = t.lastIndexOf(':')
  if (lastColon > 0) {
    const maybePort = t.slice(lastColon + 1)
    if (/^\d{1,5}$/.test(maybePort)) {
      return { host: t.slice(0, lastColon), port: maybePort }
    }
  }
  return { host: t }
}

/**
 * Password may contain `@`. Delimiter before host:port is the *last* `@` in
 * the authority, not the first (RFC userinfo otherwise requires %40).
 */
function parseMieruAuthority(base: string): {
  auth?: string
  host: string
  port?: string
} {
  const s = base.trim()
  if (!s) return { host: '' }
  const lastAt = s.lastIndexOf('@')
  if (lastAt === -1) {
    return parseMieruHostPort(s)
  }
  const auth = s.slice(0, lastAt)
  const { host, port } = parseMieruHostPort(s.slice(lastAt + 1))
  return { auth: auth || undefined, host, port }
}

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
    base,
    query: addons,
    fragment: nameRaw,
  } = splitMieruQueryAndFragment(afterScheme)
  const { auth: authRaw, host: server, port } = parseMieruAuthority(base)
  if (!String(server ?? '').trim()) {
    throw new Error('Invalid mieru uri')
  }
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

  if (!proxy.transport) {
    proxy.transport = proxy.udp === true ? 'UDP' : 'TCP'
  }
  if (proxy.transport === 'UDP' && proxy.udp === undefined) {
    proxy.udp = true
  }

  return proxy
}
