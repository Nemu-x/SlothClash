import { act, fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { useProfileModals } from '../hooks/useProfileModals'
import { renderWithProviders } from '../test/render'
import { wailsApp } from '../test/wailsMock'

import { ProfileModalsHost } from './ProfileModalsHost'

// Monaco does not render in jsdom; a textarea exposes the YAML editor's value.
vi.mock('../MonacoYamlEditor', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../MonacoYamlEditor')>()),
  MonacoYamlEditor: ({
    value,
    onChange,
  }: {
    value: string
    onChange: (next: string) => void
  }) => (
    <textarea
      data-testid="yaml-editor"
      value={value}
      onChange={(e) => onChange(e.target.value)}
    />
  ),
}))

const PROFILE = {
  id: 'p1',
  name: 'Work',
  url: 'https://sub.example/work',
  autoUpdateEnabled: false,
  autoUpdateIntervalMinutes: 120,
}

function makeProps() {
  return {
    run: vi.fn(async (action: () => Promise<any>) => {
      await action()
    }),
    refresh: vi.fn(async () => {}),
    setError: vi.fn(),
    setBanner: vi.fn(),
    pushToast: vi.fn(() => 1),
    onImported: vi.fn(),
  }
}

// The App side of the contract: useProfileModals says what is open, and the
// buttons stand in for Home / Profiles opening a dialog.
function Harness({ props }: { props: ReturnType<typeof makeProps> }) {
  const modals = useProfileModals()
  return (
    <>
      <button type="button" onClick={() => modals.openImport('connect')}>
        open-import
      </button>
      <button
        type="button"
        onClick={() => modals.setMenu({ id: 'p1', name: 'Work', x: 5, y: 5 })}
      >
        open-menu
      </button>
      <ProfileModalsHost
        modals={modals}
        profiles={[PROFILE]}
        proxyGroups={[]}
        {...props}
      />
    </>
  )
}

function setup() {
  const props = makeProps()
  renderWithProviders(<Harness props={props} />)
  return props
}

const urlInput = () => screen.getByPlaceholderText('https://…')

describe('ProfileModalsHost', () => {
  it('imports from a URL, then clears and closes the dialog', async () => {
    const importUrl = wailsApp.stub('ImportProfileFromURL', () => undefined)
    const props = setup()

    fireEvent.click(screen.getByText('open-import'))
    expect(screen.getByText('Connect needs a profile')).toBeVisible()
    fireEvent.change(urlInput(), {
      target: { value: '  https://sub.example/new  ' },
    })
    fireEvent.change(screen.getByLabelText(/Display name/), {
      target: { value: ' Mine ' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Import' }))

    await waitFor(() =>
      expect(screen.queryByText('Connect needs a profile')).toBeNull(),
    )
    expect(importUrl).toHaveBeenCalledWith('Mine', 'https://sub.example/new')
    expect(props.refresh).toHaveBeenCalledTimes(1)
    expect(props.onImported).toHaveBeenCalledTimes(1)
    expect(props.setError).toHaveBeenCalledWith('')

    // The form is reset for the next import.
    fireEvent.click(screen.getByText('open-import'))
    expect(urlInput()).toHaveValue('')
  })

  it('keeps the dialog and the input on a failed import', async () => {
    wailsApp.stub('ImportProfileFromURL', () =>
      Promise.reject(new Error('HTTP 403')),
    )
    const props = setup()

    fireEvent.click(screen.getByText('open-import'))
    fireEvent.change(urlInput(), { target: { value: 'https://bad.example' } })
    fireEvent.click(screen.getByRole('button', { name: 'Import' }))

    await waitFor(() =>
      expect(props.setError).toHaveBeenCalledWith(
        expect.stringContaining('HTTP 403'),
      ),
    )
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Import' })).toBeEnabled(),
    )
    expect(urlInput()).toHaveValue('https://bad.example')
    expect(props.onImported).not.toHaveBeenCalled()
    expect(props.refresh).toHaveBeenCalledTimes(1)
  })

  it('deletes a profile from the context menu after confirmation', async () => {
    const del = wailsApp.stub('DeleteProfile', () => undefined)
    const props = setup()

    fireEvent.click(screen.getByText('open-menu'))
    fireEvent.click(screen.getByRole('button', { name: 'Delete profile' }))
    // The menu closes; the confirm names the profile.
    expect(screen.queryByRole('button', { name: 'Delete profile' })).toBeNull()
    expect(screen.getByRole('dialog')).toHaveTextContent('Work')
    expect(del).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    expect(screen.queryByRole('dialog')).toBeNull()
    await waitFor(() => expect(del).toHaveBeenCalledWith('p1'))
    expect(props.run).toHaveBeenCalledTimes(1)
  })

  it('cancelling the delete confirm deletes nothing', () => {
    const del = wailsApp.stub('DeleteProfile', () => undefined)
    setup()
    fireEvent.click(screen.getByText('open-menu'))
    fireEvent.click(screen.getByRole('button', { name: 'Delete profile' }))
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(del).not.toHaveBeenCalled()
  })

  it('seeds the info editor from the profile and saves the edits', async () => {
    const update = wailsApp.stub('UpdateProfileInfo', () => undefined)
    const ageKey = wailsApp.stub('SetProfileAgeSecretKey', () => undefined)
    const autoUpdate = wailsApp.stub('SetProfileAutoUpdate', () => undefined)
    setup()

    fireEvent.click(screen.getByText('open-menu'))
    fireEvent.click(screen.getByRole('button', { name: 'Edit info' }))
    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveTextContent('Edit profile info')
    expect(screen.getByLabelText('Name')).toHaveValue('Work')
    expect(screen.getByLabelText('Subscription URL')).toHaveValue(
      'https://sub.example/work',
    )
    expect(screen.getByLabelText('Interval (minutes)')).toHaveValue('120')
    // autoUpdateEnabled: false -> the auto-update knob starts off.
    expect(screen.getByText('Off')).not.toHaveClass('on')

    fireEvent.change(screen.getByLabelText('Name'), {
      target: { value: '  Work 2 ' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(screen.queryByRole('dialog')).toBeNull()
    await waitFor(() =>
      expect(autoUpdate).toHaveBeenCalledWith('p1', false, 120),
    )
    expect(update).toHaveBeenCalledWith(
      'p1',
      'Work 2',
      'https://sub.example/work',
    )
    expect(ageKey).toHaveBeenCalledWith('p1', '')
  })

  describe('profile file dialog', () => {
    const editor = () => screen.getByTestId('yaml-editor')
    const openFile = () => {
      fireEvent.click(screen.getByText('open-menu'))
      fireEvent.click(screen.getByRole('button', { name: 'Edit file' }))
    }

    it('loads the current config.yaml into the editor', async () => {
      const read = wailsApp.stub('ReadProfileConfig', () => ({
        path: 'C:/data/config.yaml',
        body: 'mixed-port: 7890\n',
      }))
      setup()
      openFile()

      await waitFor(() => expect(editor()).toHaveValue('mixed-port: 7890\n'))
      expect(read).toHaveBeenCalledWith('p1')
      expect(screen.getByText('C:/data/config.yaml')).toBeVisible()
      expect(screen.queryByText(/^Read:/)).toBeNull()
    })

    it('shows the placeholder and the reason when there is no config yet', async () => {
      wailsApp.stub('ReadProfileConfig', () => ({
        path: 'C:/data/config.yaml',
        lastError: 'file not found',
      }))
      setup()
      openFile()

      await waitFor(() =>
        expect((editor() as HTMLTextAreaElement).value).toContain(
          '# config.yaml not found yet (file not found).',
        ),
      )
      expect(screen.getByText(/^Read: file not found/)).toBeVisible()
      expect(screen.getByText('C:/data/config.yaml')).toBeVisible()
    })

    it('shows the placeholder and the error when the read fails', async () => {
      wailsApp.stub('ReadProfileConfig', () =>
        Promise.reject(new Error('access denied')),
      )
      setup()
      openFile()

      await waitFor(() =>
        expect(editor()).toHaveValue(
          '# Could not load current config.yaml. Paste YAML below to overwrite.\n',
        ),
      )
      expect(screen.getByText(/^Read: Error: access denied/)).toBeVisible()
    })

    // Closed and reopened while the first read is in flight: the late first
    // result must not land in the reopened dialog.
    it('drops a read that resolves after the dialog was closed', async () => {
      const resolvers: Array<(v: unknown) => void> = []
      wailsApp.stub(
        'ReadProfileConfig',
        () => new Promise((resolve) => resolvers.push(resolve)),
      )
      setup()
      openFile()
      fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
      openFile()
      expect(resolvers).toHaveLength(2)

      await act(async () => {
        resolvers[0]({ path: 'C:/stale/config.yaml', body: 'stale: true\n' })
      })
      expect(editor()).toHaveValue('')
      expect(screen.queryByText('C:/stale/config.yaml')).toBeNull()

      await act(async () => {
        resolvers[1]({ path: 'C:/data/config.yaml', body: 'fresh: true\n' })
      })
      expect(editor()).toHaveValue('fresh: true\n')
    })

    it('drops a read that fails after the dialog was closed', async () => {
      const rejecters: Array<(e: unknown) => void> = []
      wailsApp.stub(
        'ReadProfileConfig',
        () => new Promise((_resolve, reject) => rejecters.push(reject)),
      )
      setup()
      openFile()
      fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
      openFile()
      expect(rejecters).toHaveLength(2)

      await act(async () => {
        rejecters[0](new Error('stale failure'))
      })
      expect(editor()).toHaveValue('')
      expect(screen.queryByText(/stale failure/)).toBeNull()
    })
  })
})
