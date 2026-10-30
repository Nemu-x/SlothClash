import { act, fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { splitCorpError } from '../api/corp'
import { renderWithProviders } from '../test/render'

import { CorpCertTrustModal } from './CorpCertTrustModal'

const OLD = 'pin-sha256:MCDJgwuGvXvv80C45Q2mYbX4C334x7+82GyRERoyFCI='
const NEW = 'pin-sha256:zaTdo6UzzBdQ8q9j/dX1wPikc0LbMPI93JrWPkUXHGw='

// biome-ignore lint/suspicious/noExplicitAny: test fixture for the Wails model
const changed: any = {
  connected: false,
  needsCertTrust: true,
  servercertSha256: NEW,
  certError: 'CERTIFICATE_CHANGED',
  previousServercert: OLD,
  supported: true,
  certificate: {
    host: 'vpn.corp.test',
    subject: 'CN=vpn.corp.test',
    issuer: 'CN=corp-Root CA',
    notBefore: '2026-10-29T00:00:00Z',
    notAfter: '2027-10-29T00:00:00Z',
    sha256Fingerprint: 'AB:CD',
    spkiPin: NEW,
    chainTrusted: true,
    hostnameMatches: true,
    expired: false,
    matchesReported: true,
  },
}

describe('CorpCertTrustModal', () => {
  it('shows old and new pins with an interception warning on rotation', () => {
    renderWithProviders(
      <CorpCertTrustModal
        gateway="vpn.corp.test"
        pending={changed}
        busy={false}
        onReject={vi.fn()}
        onTrust={vi.fn()}
      />,
    )
    expect(
      screen.getByRole('dialog', { name: 'Server certificate has changed' }),
    ).toBeInTheDocument()
    expect(screen.getByText(NEW)).toBeInTheDocument()
    expect(screen.getByText(OLD)).toBeInTheDocument()
    expect(screen.getByText(/intercepting your connection/)).toBeInTheDocument()
    expect(screen.getByText('CN=corp-Root CA')).toBeInTheDocument()
  })

  it('trusts only on an explicit click, with the observed pin', () => {
    vi.useFakeTimers()
    const onTrust = vi.fn()
    const onReject = vi.fn()
    renderWithProviders(
      <CorpCertTrustModal
        gateway="vpn.corp.test"
        pending={changed}
        busy={false}
        onReject={onReject}
        onTrust={onTrust}
      />,
    )
    act(() => {
      vi.advanceTimersByTime(10 * 60 * 1000)
    })
    expect(onTrust).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Reject' })).toHaveFocus()
    fireEvent.click(screen.getByRole('button', { name: 'Trust certificate' }))
    expect(onTrust).toHaveBeenCalledWith(NEW)
    vi.useRealTimers()
  })

  it('rejects on Escape and backdrop, never trusts', () => {
    const onTrust = vi.fn()
    const onReject = vi.fn()
    renderWithProviders(
      <CorpCertTrustModal
        gateway="vpn.corp.test"
        pending={changed}
        busy={false}
        onReject={onReject}
        onTrust={onTrust}
      />,
    )
    fireEvent.keyDown(document, { key: 'Escape' })
    fireEvent.click(document.querySelector('.modalOverlay') as HTMLElement)
    expect(onReject).toHaveBeenCalledTimes(2)
    expect(onTrust).not.toHaveBeenCalled()
  })

  it('warns when the probe saw a different certificate', () => {
    renderWithProviders(
      <CorpCertTrustModal
        gateway="vpn.corp.test"
        pending={{
          ...changed,
          certificate: { ...changed.certificate, matchesReported: false },
        }}
        busy={false}
        onReject={vi.fn()}
        onTrust={vi.fn()}
      />,
    )
    expect(screen.getByText(/DIFFERENT certificate/)).toBeInTheDocument()
  })
})

describe('splitCorpError', () => {
  it('splits known codes and leaves the rest untouched', () => {
    expect(splitCorpError('VPN_AUTH_FAILED: Login failed.')).toEqual({
      code: 'VPN_AUTH_FAILED',
      detail: 'Login failed.',
    })
    expect(splitCorpError('prepare OpenConnect: boom')).toEqual({
      code: '',
      detail: 'prepare OpenConnect: boom',
    })
  })
})
