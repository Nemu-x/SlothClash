import { describe, expect, it } from 'vitest'

import en from './en.json'
import ru from './ru.json'
import zh from './zh.json'

type Tree = { [key: string]: string | Tree }

const PLURAL = /_(zero|one|two|few|many|other)$/

function flatten(tree: Tree, prefix = '', out = new Map<string, string>()) {
  for (const [k, v] of Object.entries(tree)) {
    const key = prefix ? `${prefix}.${k}` : k
    if (typeof v === 'string') out.set(key, v)
    else flatten(v, key, out)
  }
  return out
}

/** key without its plural suffix -> plural categories present ([] = plain). */
function bases(strings: Map<string, string>) {
  const out = new Map<string, string[]>()
  for (const key of strings.keys()) {
    const m = PLURAL.exec(key)
    const base = m ? key.slice(0, m.index) : key
    const forms = out.get(base) ?? []
    if (m) forms.push(m[1])
    out.set(base, forms)
  }
  return out
}

const vars = (s: string) =>
  [...s.matchAll(/\{\{(\w+)\}\}/g)].map((m) => m[1]).sort()

const EN = flatten(en as Tree)
const EN_BASES = bases(EN)

describe.each([
  ['ru', ru],
  ['zh', zh],
])('locale %s', (lang, tree) => {
  const strings = flatten(tree as Tree)
  const own = bases(strings)
  // The forms i18next looks up for a count in this language.
  const categories = new Intl.PluralRules(lang).resolvedOptions()
    .pluralCategories as string[]

  it('has every English key', () => {
    expect([...EN_BASES.keys()].filter((k) => !own.has(k))).toEqual([])
  })

  it('has no key English lacks', () => {
    expect([...own.keys()].filter((k) => !EN_BASES.has(k))).toEqual([])
  })

  it('gives every pluralized English key all of its own plural forms', () => {
    const missing: string[] = []
    for (const [base, forms] of EN_BASES) {
      if (forms.length === 0) continue
      for (const c of categories)
        if (!strings.has(`${base}_${c}`)) missing.push(`${base}_${c}`)
      // A plain key next to the forms would be dead text.
      if (strings.has(base)) missing.push(`${base} (plain duplicate)`)
    }
    expect(missing).toEqual([])
  })

  it('keeps the interpolation variables of the English text', () => {
    const wrong: string[] = []
    for (const [key, text] of strings) {
      const m = PLURAL.exec(key)
      const base = m ? key.slice(0, m.index) : key
      const source =
        EN.get(key) ?? EN.get(`${base}_other`) ?? EN.get(base) ?? null
      if (source === null) continue
      // A one-form may name the number in words ("один"), so it may drop
      // {{count}}.
      const want = vars(source).filter((v) => !(m && v === 'count'))
      const got = vars(text).filter((v) => !(m && v === 'count'))
      if (want.join() !== got.join()) wrong.push(key)
    }
    expect(wrong).toEqual([])
  })
})
