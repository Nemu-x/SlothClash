import { load as parseYaml } from 'js-yaml'

export function yamlValidationError(
  text: string,
  requireMapping = true,
): string | null {
  const src = String(text ?? '').trim()
  if (!src) return null
  if (requireMapping) {
    const noComments = src
      .split('\n')
      .map((line) => line.trim())
      .filter((line) => line && !line.startsWith('#'))
    if (noComments.length === 0) return null
  }
  try {
    const parsed = parseYaml(src)
    if (
      requireMapping &&
      (parsed == null || Array.isArray(parsed) || typeof parsed !== 'object')
    ) {
      return 'YAML must be a mapping object at top-level.'
    }
    return null
  } catch (e: any) {
    return String(e?.message || e || 'Invalid YAML')
  }
}

export function friendlyErrorMessage(raw: string): string {
  const msg = String(raw ?? '').trim()
  if (!msg) return ''
  if (msg.includes('unknown policy')) {
    return `${msg}. Hint: use existing proxy group name or DIRECT/REJECT.`
  }
  if (msg.includes('unknown provider')) {
    return `${msg}. Hint: check proxy-providers names in merge template.`
  }
  if (msg.includes('unknown proxy/group')) {
    return `${msg}. Hint: check proxy-groups -> proxies/use targets.`
  }
  if (msg.includes('configuration preflight failed')) {
    return `${msg}. Hint: open profile config and verify rules and proxy-groups references.`
  }
  return msg
}
