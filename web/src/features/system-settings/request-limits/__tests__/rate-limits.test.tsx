import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { RateLimitSection } from '../rate-limit-section'

const defaults = {
  ModelRequestRateLimitEnabled: false,
  ModelRequestRateLimitDurationMinutes: 0,
  ModelRequestRateLimitCount: 100,
  ModelRequestRateLimitSuccessCount: 50,
  ModelRequestRateLimitGroup: '{"vip":[0,0]}',
}

let client: QueryClient

beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  })
})

afterEach(() => client.clear())

function SettingsHarness(props: { values: typeof defaults }) {
  const [actions, setActions] = useState<HTMLDivElement | null>(null)
  return (
    <SettingsPageProvider actionsContainer={actions}>
      <div ref={setActions} />
      <RateLimitSection defaultValues={props.values} />
    </SettingsPageProvider>
  )
}

function renderSettings(values = defaults) {
  render(
    <QueryClientProvider client={client}>
      <SettingsHarness values={values} />
    </QueryClientProvider>
  )
}

const availableGroups = {
  vip: { desc: '', ratio: 1 },
  'new-group': { desc: '', ratio: 1 },
  default: { desc: '', ratio: 1 },
  auto: { desc: '', ratio: 'auto' },
}

it('searches unconfigured groups and saves the selected group with zero counts', async () => {
  const user = userEvent.setup()
  const get = vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: availableGroups },
  })
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  renderSettings()

  const row = screen.getByRole('row', { name: /vip/i })
  expect(within(row).getAllByText('Unlimited')).toHaveLength(2)

  expect(get).not.toHaveBeenCalled()
  await user.click(screen.getByRole('button', { name: /Add group/i }))
  const dialog = screen.getByRole('dialog', { name: 'Add group rate limit' })
  const picker = within(dialog).getByRole('combobox', { name: 'Group Name' })
  await waitFor(() => expect(picker).toBeEnabled())
  expect(picker).toHaveAttribute('aria-expanded', 'false')
  await user.click(picker)
  expect(picker).toHaveAttribute('aria-expanded', 'true')
  expect(screen.queryByRole('option', { name: 'vip' })).not.toBeInTheDocument()
  expect(screen.getByRole('option', { name: 'auto' })).toBeVisible()
  await user.type(picker, 'new-')
  expect(
    screen.queryByRole('option', { name: 'default' })
  ).not.toBeInTheDocument()
  await user.keyboard('{ArrowDown}{Enter}')
  expect(picker).toHaveValue('new-group')
  expect(
    within(dialog).getByRole('spinbutton', { name: 'Max Successful Requests' })
  ).toHaveValue(0)
  await user.click(within(dialog).getByRole('button', { name: 'Add' }))

  expect(put).not.toHaveBeenCalled()
  const newRow = await screen.findByRole('row', { name: /new-group/i })
  expect(within(newRow).getAllByText('Unlimited')).toHaveLength(2)
  await user.click(screen.getByRole('button', { name: 'Save rate limits' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith('/api/option/', {
      key: 'ModelRequestRateLimitGroup',
      value: JSON.stringify({ vip: [0, 0], 'new-group': [0, 0] }, null, 2),
    })
  )
})

it('does not add an arbitrary typed name and allows correcting the selection', async () => {
  const user = userEvent.setup()
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: availableGroups },
  })
  renderSettings()

  await user.click(screen.getByRole('button', { name: /Add group/i }))
  const dialog = screen.getByRole('dialog', { name: 'Add group rate limit' })
  const picker = within(dialog).getByRole('combobox', { name: 'Group Name' })
  await waitFor(() => expect(picker).toBeEnabled())
  await user.type(picker, 'does-not-exist')
  expect(screen.getByText('No groups match your search')).toBeVisible()
  await user.keyboard('{Escape}')
  expect(picker).toHaveAttribute('aria-expanded', 'false')
  await user.click(within(dialog).getByRole('button', { name: 'Add' }))
  await waitFor(() => expect(picker).toHaveAttribute('aria-invalid', 'true'))
  expect(dialog).toBeVisible()
  expect(
    screen.queryByRole('row', { name: /does-not-exist/i })
  ).not.toBeInTheDocument()
  await user.click(picker)
  await user.click(screen.getByRole('option', { name: 'default' }))
  await user.click(within(dialog).getByRole('button', { name: 'Add' }))
  expect(await screen.findByRole('row', { name: /default/i })).toBeVisible()
})

it('allows retrying a group list error and handles a list with no unconfigured groups', async () => {
  const user = userEvent.setup()
  vi.spyOn(api, 'get')
    .mockResolvedValueOnce({ data: { success: false, message: 'Unavailable' } })
    .mockResolvedValueOnce({
      data: { success: true, data: { vip: availableGroups.vip } },
    })
  renderSettings()

  await user.click(screen.getByRole('button', { name: /Add group/i }))
  const dialog = screen.getByRole('dialog', { name: 'Add group rate limit' })
  const add = within(dialog).getByRole('button', { name: 'Add' })
  expect(await within(dialog).findByRole('alert')).toHaveTextContent(
    'Failed to load groups'
  )
  expect(add).toBeDisabled()
  await user.click(within(dialog).getByRole('button', { name: 'Retry' }))
  expect(
    await within(dialog).findByText('No unconfigured request groups available.')
  ).toBeVisible()
  expect(add).toBeDisabled()
})

it('keeps an existing rule editable without looking up its group name', async () => {
  const user = userEvent.setup()
  const get = vi.spyOn(api, 'get')
  renderSettings()

  await user.click(
    within(screen.getByRole('row', { name: /vip/i })).getByRole('button', {
      name: 'Edit',
    })
  )
  const dialog = screen.getByRole('dialog', { name: 'Edit group rate limit' })
  const group = within(dialog).getByRole('textbox', { name: 'Group Name' })
  expect(group).toHaveValue('vip')
  expect(group).toHaveAttribute('readonly')
  expect(get).not.toHaveBeenCalled()
})

it('rejects a zero enabled window and saves the window before enabling enforcement', async () => {
  const user = userEvent.setup()
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  renderSettings()

  expect(screen.getByRole('alert')).toHaveTextContent(
    'Saved limits are not enforced'
  )
  const duration = screen.getByRole('spinbutton', { name: 'Limit period' })
  await user.click(screen.getByRole('switch', { name: 'Enable rate limiting' }))
  await user.click(screen.getByRole('button', { name: 'Save rate limits' }))
  await waitFor(() => expect(duration).toHaveAttribute('aria-invalid', 'true'))
  expect(put).not.toHaveBeenCalled()

  fireEvent.change(duration, { target: { value: '1440' } })
  const success = screen.getByRole('spinbutton', {
    name: 'Max Successful Requests',
  })
  fireEvent.change(success, { target: { value: '0' } })
  await user.click(screen.getByRole('button', { name: 'Save rate limits' }))
  await waitFor(() =>
    expect(put.mock.calls.map(([, data]) => data)).toEqual([
      { key: 'ModelRequestRateLimitDurationMinutes', value: 1440 },
      { key: 'ModelRequestRateLimitSuccessCount', value: 0 },
      { key: 'ModelRequestRateLimitEnabled', value: true },
    ])
  )
})

it('rejects fractional JSON limits and accepts zero and the highest whole count', async () => {
  const user = userEvent.setup()
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  renderSettings()

  await user.click(screen.getByRole('button', { name: 'JSON Mode' }))
  const editor = screen.getByRole('textbox', {
    name: 'Additional limits by request group',
  })
  fireEvent.input(editor, { target: { value: '{"vip":[0,1.5]}' } })
  await user.click(screen.getByRole('button', { name: 'Save rate limits' }))
  await waitFor(() => expect(editor).toHaveAttribute('aria-invalid', 'true'))
  expect(put).not.toHaveBeenCalled()

  fireEvent.input(editor, { target: { value: '{"vip":[2147483647,0e0]}' } })
  await user.click(screen.getByRole('button', { name: 'Save rate limits' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith('/api/option/', {
      key: 'ModelRequestRateLimitGroup',
      value: JSON.stringify({ vip: [2147483647, 0] }, null, 2),
    })
  )
})
