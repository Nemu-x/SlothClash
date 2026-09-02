# JavaScript config override

A profile can carry a JavaScript function that rewrites its generated
configuration before the core sees it. It is the escape hatch the three
declarative editors (extend config, proxy groups, rules) cannot cover: anything
conditional or computed — "rename every node matching this pattern", "build one
group per country", "drop nodes whose name says 10× billing".

Open it from the profile's context menu → **Script (JS)**.

## The contract

```js
function main(config, ctx) {
  // config — the whole configuration, as a plain object
  // ctx    — { traffic: 'tun' | 'proxy', platform, appVersion }
  return config // whatever you return is what gets used
}
```

- The entry point must be called `main`. `const main = (config) => config` works
  too.
- It must **return an object**. Returning nothing, a string, or an array is a
  failure.
- The object you get is a copy: mutate it freely, nothing leaks back if the run
  fails.

## When it runs

```
subscription → merge templates (extend / proxy groups / rules)
             → SlothClash overlays (ports, DNS, TUN, sniffer, your settings)
             → YOUR SCRIPT                       ← here
             → invariants re-applied             ← see below
             → validation → the core
```

Running late is the point: you see `dns`, `tun`, `sniffer` and the rest exactly
as the core would have received them, so you can override our decisions instead
of having them overwrite yours.

The script runs on every generation for that profile — connecting, switching
traffic mode, editing the profile, refreshing the subscription.

## What a script can and cannot change

| Yours | Re-applied after your script, always |
| --- | --- |
| `dns.*`, `tun.*`, `sniffer.*` | `mixed-port` (and `socks-port`/`port` staying 0) |
| `rules`, `rule-providers` | `external-controller` — including its **removal** when the app runs the core without one |
| `proxies`, `proxy-groups`, `proxy-providers` | `secret` |
| `mode`, `log-level`, `profile.*`, `allow-lan`, everything else | the corporate-VPN split (`route-exclude-address`, split DNS) while a corporate sidecar is active |

The right-hand column is how the app talks to its own core and how a second live
tunnel keeps working. Change those and your change is simply overwritten — no
error, no argument.

## Failure behavior

A broken script never blocks a connect. On **any** failure — syntax error,
missing `main`, thrown exception, timeout, non-object return, a value that
cannot be written to the config — the script's output is discarded, the config
is generated as if the profile had no script, and the reason is recorded on the
profile (with line and column when the engine reports them) and written to the
log. The profile shows a red **JS** badge until a run succeeds.

## Limits

| Limit | Value |
| --- | --- |
| Wall-clock per run | 3 seconds, then the script is interrupted |
| Call stack | 2048 frames |
| Script size | 256 KiB |
| Captured `console` output | 200 lines / 16 KiB, then truncated with a marker |

## The sandbox

The interpreter is [goja](https://github.com/dop251/goja) — ES5.1 plus much of
ES2015+ (`let`/`const`, arrow functions, template literals, `Map`, `Set`,
`JSON`, `RegExp`). A fresh interpreter is built for every run, so nothing
carries over between generations or profiles.

There is **no host access at all**: no `require`, `fetch`, `process`,
`setTimeout`, `Deno`, `Buffer`, filesystem or network. The only thing added on
top of plain JavaScript is `console.log/warn/error/info/debug`, captured into a
bounded buffer and shown next to the preview.

## Values

Numbers are normalized on the way back: an integral number is written as an
integer, so `config['mixed-port'] * 1` stays `7890` and never becomes `7890.0`.
Genuinely fractional numbers stay fractional.

These are refused (the whole run fails, the path is named in the error):

- `NaN`, `Infinity`, `-Infinity`
- functions
- cyclic references
- `Date`, `Map`, `Set`, `RegExp`, `Promise` and friends — they would silently
  serialize to `{}`; convert them yourself (`String(date)`, `[...map]`)

## Preview

**Preview** runs your script through the real generation pipeline twice, with
and without it, and shows a side-by-side diff of the two configs plus anything
you logged. It writes nothing, restarts nothing and does not touch the running
connection — it is safe to press while connected.

## Examples

**Rename nodes by pattern**

```js
function main(config) {
  config.proxies = config.proxies.map(function (p) {
    p.name = p.name.replace(/\s*\|\s*\d+x\s*$/i, '')
    return p
  })
  return config
}
```

**A group per country, from a flag prefix**

```js
function main(config) {
  const byCountry = {}
  config.proxies.forEach(function (p) {
    const flag = (p.name.match(/^\p{Regional_Indicator}{2}/u) || ['🌐'])[0]
    ;(byCountry[flag] = byCountry[flag] || []).push(p.name)
  })
  const groups = Object.keys(byCountry).map(function (flag) {
    return { name: flag, type: 'url-test', interval: 300, proxies: byCountry[flag] }
  })
  config['proxy-groups'] = groups.concat(config['proxy-groups'] || [])
  return config
}
```

**Drop nodes by name, keeping the groups consistent**

```js
function main(config) {
  const banned = /trial|expire/i
  const keep = config.proxies.filter(function (p) { return !banned.test(p.name) })
  const names = keep.map(function (p) { return p.name })
  console.log('dropped ' + (config.proxies.length - keep.length) + ' nodes')
  config.proxies = keep
  config['proxy-groups'] = (config['proxy-groups'] || []).map(function (g) {
    if (Array.isArray(g.proxies)) {
      g.proxies = g.proxies.filter(function (n) {
        return names.indexOf(n) >= 0 || ['DIRECT', 'REJECT', 'PASS'].indexOf(n) >= 0
      })
    }
    return g
  })
  return config
}
```

**TUN-only tweak**

```js
function main(config, ctx) {
  if (ctx.traffic === 'tun') {
    config.tun.mtu = 1500
  }
  return config
}
```

## Where scripts come from

Only from your own editor, on your own machine. A subscription body, a provider
header, a share link or an imported profile can never set, modify or enable a
script — that would be provider-controlled code execution inside your config.
This is enforced in code and locked by tests.
