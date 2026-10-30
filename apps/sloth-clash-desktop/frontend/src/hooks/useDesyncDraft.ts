import { useQuery } from '@tanstack/react-query'
import { useCallback, useEffect, useRef, useState } from 'react'

import {
  DraftDesyncAddCustomDomain,
  DraftDesyncAddRule,
  DraftDesyncAddService,
  DraftDesyncUpdateRule,
  NewDesyncDraft,
  type DesyncRuleInput,
  type DesyncSettings,
} from '../api/desync'

/**
 * The Desync settings edits a panel makes: saved at once for the active
 * profile (useDesyncStatus), or kept in a draft (useDesyncDraft). Every
 * write resolves to false when it did not run.
 */
export type DesyncEditor = {
  /** Apply `settings` (the full object; the custom list is not changed). */
  save: (settings: DesyncSettings) => Promise<boolean>
  addCustom: (domain: string) => Promise<boolean>
  /** Add a rule ("My rules"), enabled. */
  addRule: (rule: DesyncRuleInput) => Promise<boolean>
  /** Replace rule `id`, keeping its on/off state. */
  updateRule: (id: string, rule: DesyncRuleInput) => Promise<boolean>
  /** Resolves to the domains of the plain entries the service replaced. */
  addService: (id: string) => Promise<string[] | false>
  removeCustom: (id: string) => Promise<boolean>
  setCustomEnabled: (id: string, enabled: boolean) => Promise<boolean>
}

export type DesyncDraft = DesyncEditor & {
  /** The draft; null while the defaults load. */
  settings: DesyncSettings | null
}

const DRAFT_DEFAULTS_KEY = ['desync-draft-defaults'] as const

/**
 * What a new Desync-only profile will unblock, chosen in the onboarding
 * before the profile exists. Nothing is saved: the draft lives in this hook
 * until "Create profile" hands it to CreateDesyncOnlyProfile, so Cancel
 * leaves every profile as it was. Adding a site or a popular service goes
 * through the backend (the same validation as a saved list) without saving.
 */
export function useDesyncDraft(enabled = true): DesyncDraft {
  const defaults = useQuery({
    queryKey: DRAFT_DEFAULTS_KEY,
    queryFn: NewDesyncDraft,
    enabled,
    staleTime: Infinity,
  })
  const [edited, setEdited] = useState<DesyncSettings | null>(null)
  const settings = edited ?? defaults.data ?? null
  // The latest draft for the async writes (a write starts from what the
  // previous one produced, not from a stale render).
  const latestRef = useRef(settings)
  useEffect(() => {
    latestRef.current = settings
  })

  const update = useCallback((next: DesyncSettings) => {
    latestRef.current = next
    setEdited(next)
    return true
  }, [])

  const save = useCallback(
    async (next: DesyncSettings) =>
      update({ ...next, custom: latestRef.current?.custom }),
    [update],
  )
  const addCustom = useCallback(
    async (domain: string) => {
      const current = latestRef.current
      if (!current) return false
      return update(await DraftDesyncAddCustomDomain(current, domain))
    },
    [update],
  )
  const addRule = useCallback(
    async (rule: DesyncRuleInput) => {
      const current = latestRef.current
      if (!current) return false
      return update(await DraftDesyncAddRule(current, rule))
    },
    [update],
  )
  const updateRule = useCallback(
    async (id: string, rule: DesyncRuleInput) => {
      const current = latestRef.current
      if (!current) return false
      return update(await DraftDesyncUpdateRule(current, id, rule))
    },
    [update],
  )
  const addService = useCallback(
    async (id: string): Promise<string[] | false> => {
      const current = latestRef.current
      if (!current) return false
      const next = await DraftDesyncAddService(current, id)
      const kept = new Set((next.custom ?? []).map((e) => e.id))
      update(next)
      return (current.custom ?? [])
        .filter((e) => !e.service && !kept.has(e.id))
        .map((e) => e.domain)
    },
    [update],
  )
  const removeCustom = useCallback(
    async (id: string) => {
      const current = latestRef.current
      if (!current) return false
      return update({
        ...current,
        custom: (current.custom ?? []).filter((e) => e.id !== id),
      })
    },
    [update],
  )
  const setCustomEnabled = useCallback(
    async (id: string, on: boolean) => {
      const current = latestRef.current
      if (!current) return false
      return update({
        ...current,
        custom: (current.custom ?? []).map((e) =>
          e.id === id ? { ...e, enabled: on } : e,
        ),
      })
    },
    [update],
  )

  return {
    settings,
    save,
    addCustom,
    addRule,
    updateRule,
    addService,
    removeCustom,
    setCustomEnabled,
  }
}
