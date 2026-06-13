import { useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

export type ProfileMenuTarget = {
  id: string
  name: string
  x: number
  y: number
}

export function ProfileContextMenu({
  target,
  profile,
  onUpdate,
  onEditInfo,
  onCopyUrl,
  onOpenRules,
  onDelete,
  onOpenExtendConfig,
  onOpenProxyGroups,
  onOpenEditFile,
}: {
  target: ProfileMenuTarget | null
  // The full profile record (or null) — needed to know if the URL is set and
  // for the actual `url` value used by "Copy subscription URL".
  profile: { url?: string } | null
  onUpdate: (id: string) => void
  onEditInfo: (id: string, name: string, url: string) => void
  onCopyUrl: (url: string) => void
  onOpenRules: (id: string, name: string) => void
  onDelete: (id: string, name: string) => void
  onOpenExtendConfig: (id: string, name: string) => void
  onOpenProxyGroups: (id: string, name: string) => void
  onOpenEditFile: (id: string, name: string) => void
}) {
  const { t } = useTranslation()
  // Hooks must run unconditionally — declare them before the early return.
  const menuRef = useRef<HTMLDivElement | null>(null)
  // Position is initialised to the click coordinates and then clamped against
  // the viewport after the first paint so the menu cannot get clipped off
  // the bottom / right edge of the window. We fall back to the raw click
  // coordinates while the measurement is pending (target.x/y).
  const [pos, setPos] = useState<{ left: number; top: number } | null>(null)
  // useLayoutEffect + setState is the canonical pattern for measure-then-
  // position: the menu's height depends on its localised item list, which
  // only resolves in the DOM, so we read getBoundingClientRect after layout
  // and push the clamped position back as state for the next paint. The
  // first-frame flicker is suppressed via `visibility: hidden` below.
  useLayoutEffect(() => {
    if (!target || !menuRef.current) {
      // eslint-disable-next-line @eslint-react/set-state-in-effect
      setPos(null)
      return
    }
    const el = menuRef.current
    const rect = el.getBoundingClientRect()
    const margin = 8
    const vw = window.innerWidth
    const vh = window.innerHeight
    let left = target.x
    let top = target.y
    if (left + rect.width + margin > vw) {
      // Prefer opening to the LEFT of the click when the menu would cross
      // the right edge — matches native context-menu behaviour.
      left = Math.max(margin, target.x - rect.width)
    }
    if (top + rect.height + margin > vh) {
      // Flip upward: anchor the menu's bottom to the click point so the
      // first item stays at eye level and the whole list fits in-window.
      top = Math.max(margin, target.y - rect.height)
    }
    // Final safety clamps in case the menu is larger than the viewport.
    left = Math.max(margin, Math.min(left, vw - rect.width - margin))
    top = Math.max(margin, Math.min(top, vh - rect.height - margin))
    // eslint-disable-next-line @eslint-react/set-state-in-effect
    setPos({ left, top })
  }, [target])

  if (!target) return null
  const hasUrl = Boolean(String(profile?.url ?? '').trim())
  return (
    <div
      ref={menuRef}
      className="ctxMenu"
      style={{
        left: pos?.left ?? target.x,
        top: pos?.top ?? target.y,
        // Hide the first paint frame so the user never sees the menu in
        // the unclamped position before useLayoutEffect re-positions it.
        visibility: pos ? 'visible' : 'hidden',
      }}
      onClick={(e) => e.stopPropagation()}
    >
      <div className="ctxTitle">{target.name}</div>
      <button
        type="button"
        className="ctxItem"
        disabled={!hasUrl}
        onClick={() => onUpdate(target.id)}
      >
        {t('ui.profiles.contextMenu.updateNow')}
      </button>
      <button
        type="button"
        className="ctxItem"
        onClick={() =>
          onEditInfo(target.id, target.name, String(profile?.url ?? ''))
        }
      >
        {t('ui.profiles.contextMenu.editInfo')}
      </button>
      <button
        type="button"
        className="ctxItem"
        disabled={!hasUrl}
        onClick={() => onCopyUrl(String(profile?.url ?? ''))}
      >
        {t('ui.profiles.contextMenu.copySubscriptionUrl')}
      </button>
      <button
        type="button"
        className="ctxItem"
        onClick={() => onOpenRules(target.id, target.name)}
      >
        {t('ui.profiles.contextMenu.rules')}
      </button>
      <button
        type="button"
        className="ctxItem"
        onClick={() => onDelete(target.id, target.name)}
      >
        {t('ui.profiles.contextMenu.deleteProfile')}
      </button>
      <div className="ctxSection">{t('ui.profiles.contextMenu.advanced')}</div>
      <button
        type="button"
        className="ctxItem ctxItemSub"
        onClick={() => onOpenExtendConfig(target.id, target.name)}
      >
        {t('ui.profiles.contextMenu.extendConfig')}
      </button>
      <button
        type="button"
        className="ctxItem ctxItemSub"
        onClick={() => onOpenProxyGroups(target.id, target.name)}
      >
        {t('ui.profiles.contextMenu.proxyGroups')}
      </button>
      <button
        type="button"
        className="ctxItem ctxItemSub"
        onClick={() => onOpenEditFile(target.id, target.name)}
      >
        {t('ui.profiles.contextMenu.editFile')}
      </button>
    </div>
  )
}
