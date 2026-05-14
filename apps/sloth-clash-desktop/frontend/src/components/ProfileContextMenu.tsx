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
  if (!target) return null
  const hasUrl = Boolean(String(profile?.url ?? '').trim())
  return (
    <div
      className="ctxMenu"
      style={{ left: target.x, top: target.y }}
      onClick={(e) => e.stopPropagation()}
    >
      <div className="ctxTitle">{target.name}</div>
      <button
        type="button"
        className="ctxItem"
        disabled={!hasUrl}
        onClick={() => onUpdate(target.id)}
      >
        Update profile now
      </button>
      <button
        type="button"
        className="ctxItem"
        onClick={() =>
          onEditInfo(target.id, target.name, String(profile?.url ?? ''))
        }
      >
        Edit info
      </button>
      <button
        type="button"
        className="ctxItem"
        disabled={!hasUrl}
        onClick={() => onCopyUrl(String(profile?.url ?? ''))}
      >
        Copy subscription URL
      </button>
      <button
        type="button"
        className="ctxItem"
        onClick={() => onOpenRules(target.id, target.name)}
      >
        Rules
      </button>
      <button
        type="button"
        className="ctxItem"
        onClick={() => onDelete(target.id, target.name)}
      >
        Delete profile
      </button>
      <div className="ctxSection">Advanced</div>
      <button
        type="button"
        className="ctxItem ctxItemSub"
        onClick={() => onOpenExtendConfig(target.id, target.name)}
      >
        Extend config
      </button>
      <button
        type="button"
        className="ctxItem ctxItemSub"
        onClick={() => onOpenProxyGroups(target.id, target.name)}
      >
        Proxy groups
      </button>
      <button
        type="button"
        className="ctxItem ctxItemSub"
        onClick={() => onOpenEditFile(target.id, target.name)}
      >
        Edit file
      </button>
    </div>
  )
}
