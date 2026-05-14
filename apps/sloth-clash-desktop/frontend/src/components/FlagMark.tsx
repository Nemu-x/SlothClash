import { useState } from 'react'

import { isoToFlagEmoji } from '../utils/proxyNames'

export function FlagMark({
  iso2,
  width,
  height,
}: {
  iso2: string
  width: number
  height: number
}) {
  const iso = String(iso2 ?? '').toUpperCase()
  const emoji = isoToFlagEmoji(iso)
  const [imgVisible, setImgVisible] = useState(Boolean(iso))
  if (!iso) return null
  return (
    <>
      {imgVisible ? (
        <img
          src={`https://flagcdn.com/w20/${iso.toLowerCase()}.png`}
          alt=""
          width={width}
          height={height}
          loading="lazy"
          decoding="async"
          referrerPolicy="no-referrer"
          onError={() => setImgVisible(false)}
        />
      ) : null}
      {!imgVisible && emoji ? (
        <span className="proxyFlagEmoji">{emoji}</span>
      ) : null}
      {!imgVisible && !emoji ? (
        <span className="proxyFlagIso">{iso}</span>
      ) : null}
    </>
  )
}
