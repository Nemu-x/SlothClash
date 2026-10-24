import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  GetDesyncUDPStatus,
  ReconnectDiscordUDP,
  type DesyncUDPStatus,
} from '../api/desync'

export function DesyncUDPStatusPanel({
  enabled,
  connected,
}: {
  enabled: boolean
  connected: boolean
}) {
  const { t } = useTranslation()
  const [status, setStatus] = useState<DesyncUDPStatus | null>(null)
  const [error, setError] = useState('')
  const [resetting, setResetting] = useState(false)
  const lifecycleRef = useRef({ mounted: true, generation: 0 })

  useEffect(() => {
    const life = lifecycleRef.current
    life.mounted = true
    return () => {
      life.mounted = false
    }
  }, [])

  useEffect(() => {
    const life = lifecycleRef.current
    const generation = ++life.generation
    let timer: ReturnType<typeof setInterval> | undefined
    let pending = false
    async function refresh() {
      if (pending) return
      pending = true
      try {
        const next = await GetDesyncUDPStatus()
        if (life.mounted && generation === life.generation) {
          setStatus(next)
          setError('')
        }
      } catch (err: unknown) {
        if (life.mounted && generation === life.generation)
          setError(String(err))
      } finally {
        pending = false
      }
    }
    if (enabled && connected) {
      void refresh()
      timer = setInterval(() => void refresh(), 4000)
    }
    return () => {
      life.generation++
      if (timer) clearInterval(timer)
    }
  }, [enabled, connected])

  async function reconnect() {
    const life = lifecycleRef.current
    const generation = life.generation
    setResetting(true)
    setError('')
    try {
      await ReconnectDiscordUDP()
      const next = await GetDesyncUDPStatus()
      if (life.mounted && life.generation === generation) setStatus(next)
    } catch (err: unknown) {
      if (life.mounted && life.generation === generation) setError(String(err))
    } finally {
      if (life.mounted) setResetting(false)
    }
  }

  if (!enabled) return null
  return (
    <div className="desyncUDPStatus">
      <p className="small muted">{t('desync.udpHint')}</p>
      {status && connected ? (
        <>
          <p className="small">
            {t('desync.udpFlows', {
              direct: status.directFlows,
              profile: status.profileFlows,
            })}
          </p>
          {status.profileRoutes.length > 0 ? (
            <p className="small muted">{status.profileRoutes.join(', ')}</p>
          ) : null}
          {status.countersAvailable ? (
            <p className="small">
              {t('desync.udpCounters', {
                fake: status.counters.fakePackets,
                packets: status.counters.packets,
                errors: status.counters.fakeErrors,
              })}
            </p>
          ) : null}
          {status.lastError ? (
            <p className="error small">{status.lastError}</p>
          ) : null}
        </>
      ) : null}
      <button
        type="button"
        className="btn ghost"
        disabled={!connected || resetting}
        onClick={() => void reconnect()}
      >
        {t(resetting ? 'desync.udpReconnecting' : 'desync.udpReconnect')}
      </button>
      {error ? (
        <p className="error small" role="alert">
          {error}
        </p>
      ) : null}
    </div>
  )
}
