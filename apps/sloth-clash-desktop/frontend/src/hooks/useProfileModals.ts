import { useEffect, useState } from 'react'

import type { ProfileMenuTarget } from '../components/ProfileContextMenu'
import type { ProfileEditInfoTarget } from '../components/ProfileEditInfoModal'
import type { ProfileModalTarget } from '../components/ProfileMergeModal'
import type { ImportModalReason } from '../types/app'

export type ProfileModals = {
  importOpen: boolean
  importReason: ImportModalReason
  openImport: (reason: ImportModalReason) => void
  closeImport: () => void
  menu: ProfileMenuTarget | null
  setMenu: (target: ProfileMenuTarget | null) => void
  merge: ProfileModalTarget | null
  setMerge: (target: ProfileModalTarget | null) => void
  file: ProfileModalTarget | null
  setFile: (target: ProfileModalTarget | null) => void
  proxy: ProfileModalTarget | null
  setProxy: (target: ProfileModalTarget | null) => void
  rules: ProfileModalTarget | null
  setRules: (target: ProfileModalTarget | null) => void
  script: ProfileModalTarget | null
  setScript: (target: ProfileModalTarget | null) => void
  editInfo: ProfileEditInfoTarget | null
  setEditInfo: (target: ProfileEditInfoTarget | null) => void
  remove: ProfileModalTarget | null
  setRemove: (target: ProfileModalTarget | null) => void
  /**
   * A YAML editor (merge template, config file, proxy groups, rules) is open.
   * App pauses its state refresh and proxy polling while it is, so a refresh
   * cannot disturb an unsaved edit.
   */
  isAnyEditorModalOpen: boolean
}

/**
 * Which profile modal (or the context menu / import dialog) is open, and for
 * which profile. The drafts edited inside them live in ProfileModalsHost.
 *
 * Held by App rather than the host: App opens the import dialog itself
 * (Connect without a profile) and reads isAnyEditorModalOpen in the same
 * render the editor opens.
 */
export function useProfileModals(): ProfileModals {
  const [importOpen, setImportOpen] = useState(false)
  const [importReason, setImportReason] = useState<ImportModalReason>('manual')
  const [menu, setMenu] = useState<ProfileMenuTarget | null>(null)
  const [merge, setMerge] = useState<ProfileModalTarget | null>(null)
  const [file, setFile] = useState<ProfileModalTarget | null>(null)
  // Proxy / Rules edit drafts live inside ProfileProxyModal / ProfileRulesModal.
  const [proxy, setProxy] = useState<ProfileModalTarget | null>(null)
  const [rules, setRules] = useState<ProfileModalTarget | null>(null)
  const [script, setScript] = useState<ProfileModalTarget | null>(null)
  const [editInfo, setEditInfo] = useState<ProfileEditInfoTarget | null>(null)
  const [remove, setRemove] = useState<ProfileModalTarget | null>(null)

  useEffect(() => {
    if (!menu) return
    const close = (e: MouseEvent) => {
      const el = e.target as HTMLElement | null
      if (el?.closest?.('.ctxMenu')) return
      setMenu(null)
    }
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [menu])

  return {
    importOpen,
    importReason,
    openImport: (reason) => {
      setImportReason(reason)
      setImportOpen(true)
    },
    closeImport: () => setImportOpen(false),
    menu,
    setMenu,
    merge,
    setMerge,
    file,
    setFile,
    proxy,
    setProxy,
    rules,
    setRules,
    script,
    setScript,
    editInfo,
    setEditInfo,
    remove,
    setRemove,
    isAnyEditorModalOpen: Boolean(merge || file || proxy || rules),
  }
}
