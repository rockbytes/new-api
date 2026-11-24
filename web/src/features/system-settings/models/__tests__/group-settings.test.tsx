import { zodResolver } from '@hookform/resolvers/zod'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  fireEvent,
  render,
  screen,
  within,
  waitFor,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { describe, expect, it, vi } from 'vitest'
import { z } from 'zod'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { positiveIntegerSchema } from '../../utils/numeric-field'
import { GroupRatioForm } from '../group-ratio-form'
import { RatioSettingsCard } from '../ratio-settings-card'

const defaults = {
  GroupRatio: '{"default":1,"vip":0.8}',
  TopupGroupRatio: '{"vip":1.2}',
  GroupNotes: '{}',
  GroupGroupRatio: '{}',
  AutoGroups: '["default","vip"]',
  MaxTokenAutoGroups: 5,
  DefaultUseAutoGroup: false,
  GroupSpecialUsableGroup: '{}',
}

const schema = z.object({
  GroupRatio: z.string(),
  GroupNotes: z.string(),
  TopupGroupRatio: z.string(),
  GroupGroupRatio: z.string(),
  AutoGroups: z.string(),
  MaxTokenAutoGroups: positiveIntegerSchema('Enter a positive integer'),
  DefaultUseAutoGroup: z.boolean(),
  GroupSpecialUsableGroup: z.string(),
})

function Fixture(props: {
  onSave?: (values: typeof defaults) => Promise<void>
  initial?: Partial<typeof defaults>
  isSaving?: boolean
}) {
  const [actions, setActions] = useState<HTMLDivElement | null>(null)
  const form = useForm({
    defaultValues: { ...defaults, ...props.initial },
    resolver: zodResolver(schema),
  })
  return (
    <SettingsPageProvider actionsContainer={actions}>
      <div ref={setActions} />
      <GroupRatioForm
        form={form}
        onSave={props.onSave ?? (async () => {})}
        isSaving={props.isSaving ?? false}
      />
    </SettingsPageProvider>
  )
}

function RatioSettingsFixture() {
  const [actions, setActions] = useState<HTMLDivElement | null>(null)
  return (
    <SettingsPageProvider actionsContainer={actions}>
      <div ref={setActions} />
      <RatioSettingsCard
        modelDefaults={{
          ModelPrice: '{}',
          ModelRatio: '{}',
          CacheRatio: '{}',
          CreateCacheRatio: '{}',
          CompletionRatio: '{}',
          ImageRatio: '{}',
          AudioRatio: '{}',
          AudioCompletionRatio: '{}',
          ExposeRatioEnabled: false,
          BillingMode: '{}',
          BillingExpr: '{}',
          PluginBillingExpr: '{}',
        }}
        groupDefaults={{
          ...defaults,
          GroupNotes: '{"vip":"first","default":"second"}',
        }}
        toolPricesDefault='{}'
        visibleTabs={['groups']}
      />
    </SettingsPageProvider>
  )
}

it('saves notes independently and retries only uncommitted fields after partial failure', async () => {
  const user = userEvent.setup()
  const client = new QueryClient({
    defaultOptions: {
      queries: { enabled: false },
      mutations: { retry: false },
    },
  })
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  const view = render(
    <QueryClientProvider client={client}>
      <RatioSettingsFixture />
    </QueryClientProvider>
  )
  const save = () =>
    user.click(screen.getByRole('button', { name: 'Save group settings' }))
  try {
    const note = screen.getByRole('textbox', { name: 'Internal note for vip' })
    fireEvent.change(note, { target: { value: 'saved' } })
    await save()
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Save group settings' })
      ).toBeEnabled()
    )
    expect(put.mock.calls).toEqual([
      [
        '/api/option/',
        { key: 'GroupNotes', value: '{"default":"second","vip":"saved"}' },
      ],
    ])
    put.mockClear()
    put.mockResolvedValueOnce({ data: { success: true } })
    put.mockResolvedValueOnce({
      data: { success: false, message: 'Write failed' },
    })
    fireEvent.change(screen.getAllByRole('spinbutton', { name: 'Ratio' })[1], {
      target: { value: '0.5' },
    })
    fireEvent.change(note, { target: { value: 'retry me' } })
    await save()
    expect(
      await screen.findByText(
        'Some group settings were saved. Retry to save the remaining changes.'
      )
    ).toBeVisible()
    expect(note).toHaveValue('retry me')
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Save group settings' })
      ).toBeEnabled()
    )
    expect(put.mock.calls.map(([, body]) => body)).toEqual([
      { key: 'GroupRatio', value: '{"default":1,"vip":0.5}' },
      { key: 'GroupNotes', value: '{"default":"second","vip":"retry me"}' },
    ])
    put.mockClear()
    await save()
    await waitFor(() =>
      expect(put.mock.calls).toEqual([
        [
          '/api/option/',
          { key: 'GroupNotes', value: '{"default":"second","vip":"retry me"}' },
        ],
      ])
    )
  } finally {
    view.unmount()
    client.clear()
    put.mockRestore()
  }
})

describe('group settings workspace', () => {
  it('filters by group name and clears search without discarding edits', async () => {
    const user = userEvent.setup()
    render(<Fixture />)
    const search = screen.getByRole('textbox', {
      name: 'Search groups...',
    })
    await user.type(search, 'VIP')
    expect(screen.getAllByRole('textbox', { name: 'Group name' })).toHaveLength(
      1
    )
    expect(screen.getByRole('textbox', { name: 'Group name' })).toHaveValue(
      'vip'
    )
    await user.clear(screen.getByRole('spinbutton', { name: 'Ratio' }))
    await user.type(screen.getByRole('spinbutton', { name: 'Ratio' }), '0.6')
    await user.click(screen.getByRole('button', { name: 'Clear search' }))
    expect(screen.getAllByRole('textbox', { name: 'Group name' })).toHaveLength(
      2
    )
    expect(screen.getAllByRole('spinbutton', { name: 'Ratio' })[1]).toHaveValue(
      0.6
    )
    await user.type(search, 'missing')
    expect(screen.getByText('No results found')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Add group' }))
    expect(search).toHaveValue('')
    expect(
      screen
        .getAllByRole<HTMLInputElement>('textbox', { name: 'Group name' })
        .map((input) => input.value)
    ).toEqual(['group_1', 'default', 'vip'])
    expect(screen.getAllByRole('spinbutton', { name: 'Ratio' })[2]).toHaveValue(
      0.6
    )
  })

  it('prepends consecutive new groups and saves them without changing existing settings', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(<Fixture onSave={onSave} />)

    await user.click(screen.getByRole('button', { name: 'Add group' }))
    await user.click(screen.getByRole('button', { name: 'Add group' }))
    expect(
      screen
        .getAllByRole<HTMLInputElement>('textbox', { name: 'Group name' })
        .map((input) => input.value)
    ).toEqual(['group_2', 'group_1', 'default', 'vip'])

    await user.click(
      screen.getByRole('button', { name: 'Save group settings' })
    )
    await waitFor(() => expect(onSave).toHaveBeenCalled())
    const saved = onSave.mock.calls[0][0]
    expect(JSON.parse(saved.GroupRatio)).toEqual({
      group_2: 1,
      group_1: 1,
      default: 1,
      vip: 0.8,
    })
    expect(JSON.parse(saved.TopupGroupRatio)).toEqual({ vip: 1.2 })
    expect(saved.AutoGroups).toBe(defaults.AutoGroups)
  })

  it('keeps unfinished visibility rules while switching sections and supports keyboard tabs', async () => {
    const user = userEvent.setup()
    render(
      <Fixture
        initial={{
          GroupSpecialUsableGroup: '{"vip":{"+:default":"Standard"}}',
        }}
      />
    )
    const pricing = screen.getByRole('tab', { name: 'Pricing groups' })
    pricing.focus()
    await user.keyboard('{ArrowRight}{Enter}')
    expect(
      screen.getByRole('tab', { name: 'Special ratio rules' })
    ).toHaveAttribute('aria-selected', 'true')
    expect(
      screen.queryByRole('table', { name: 'Pricing groups' })
    ).not.toBeInTheDocument()
    await user.click(screen.getByRole('tab', { name: 'Group visibility' }))
    await user.click(screen.getByRole('button', { name: 'Add rule' }))
    expect(
      screen.getAllByRole('combobox', { name: 'Group name' })
    ).toHaveLength(2)
    await user.click(pricing)
    await user.click(screen.getByRole('tab', { name: 'Group visibility' }))
    expect(
      screen.getAllByRole('combobox', { name: 'Group name' })
    ).toHaveLength(2)
    const toggle = screen.getByRole('button', { name: 'Rules for vip' })
    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await user.click(screen.getByRole('button', { name: 'Add rule' }))
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
  })

  it('reorders and removes auto groups with named controls and saves across sections', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(<Fixture onSave={onSave} />)
    await user.click(screen.getByRole('tab', { name: 'Auto group order' }))
    const list = screen.getByRole('list', { name: 'Auto group order' })
    expect(
      screen.getByRole('button', { name: 'Move default up' })
    ).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Move vip down' })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'Move vip up' }))
    expect(within(list).getAllByRole('listitem')[0]).toHaveTextContent('vip')
    await user.click(
      screen.getByRole('switch', { name: 'Default to auto groups' })
    )
    await user.click(screen.getByRole('button', { name: 'Remove default' }))
    expect(screen.getByRole('button', { name: 'Move vip up' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Move vip down' })).toBeDisabled()
    await user.click(screen.getByRole('tab', { name: 'Pricing groups' }))
    await user.click(
      screen.getByRole('button', { name: 'Save group settings' })
    )
    await waitFor(() => expect(onSave).toHaveBeenCalled())
    expect(onSave.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        AutoGroups: '[\n  "vip"\n]',
        DefaultUseAutoGroup: true,
      })
    )
  })

  it('reorders from the drag handle with arrow keys, preserves unknown groups and saves the new order', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(
      <Fixture
        onSave={onSave}
        initial={{
          AutoGroups: '["default","vip","retired"]',
          MaxTokenAutoGroups: 1,
        }}
      />
    )
    await user.click(screen.getByRole('tab', { name: 'Auto group order' }))
    const list = screen.getByRole('list', { name: 'Auto group order' })
    const handle = screen.getByRole('button', {
      name: 'Drag default to reorder',
    })
    handle.focus()
    await user.keyboard('{ArrowUp}')
    expect(within(list).getAllByRole('listitem')[0]).toHaveTextContent(
      'default'
    )
    await user.keyboard('{ArrowDown}{ArrowDown}{ArrowDown}')
    expect(within(list).getAllByRole('listitem')[2]).toHaveTextContent(
      'default'
    )
    expect(handle).toHaveFocus()
    await user.keyboard('{ArrowUp}')
    expect(within(list).getAllByRole('listitem')[1]).toHaveTextContent(
      'default'
    )
    expect(screen.getByText('Not in pricing table')).toBeVisible()
    await user.click(screen.getByRole('tab', { name: 'Pricing groups' }))
    await user.click(
      screen.getByRole('button', { name: 'Save group settings' })
    )
    await waitFor(() => expect(onSave).toHaveBeenCalled())
    expect(JSON.parse(onSave.mock.calls[0][0].AutoGroups)).toEqual([
      'vip',
      'default',
      'retired',
    ])
  })

  it('keeps a single auto group unchanged when using its drag handle keyboard controls', async () => {
    const user = userEvent.setup()
    render(<Fixture initial={{ AutoGroups: '["default"]' }} />)
    await user.click(screen.getByRole('tab', { name: 'Auto group order' }))
    const handle = screen.getByRole('button', {
      name: 'Drag default to reorder',
    })
    handle.focus()
    await user.keyboard('{ArrowUp}{ArrowDown}')
    const list = screen.getByRole('list', { name: 'Auto group order' })
    expect(within(list).getAllByRole('listitem')).toHaveLength(1)
    expect(within(list).getByRole('listitem')).toHaveTextContent('default')
    expect(handle).toHaveFocus()
    expect(
      screen.getByRole('button', { name: 'Move default up' })
    ).toBeDisabled()
    expect(
      screen.getByRole('button', { name: 'Move default down' })
    ).toBeDisabled()
  })

  it('opens the auto section and focuses its invalid limit when saving from another section', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(<Fixture onSave={onSave} />)
    await user.click(screen.getByRole('tab', { name: 'Auto group order' }))
    fireEvent.change(
      screen.getByRole('spinbutton', {
        name: 'Maximum custom groups per token',
      }),
      { target: { value: '0' } }
    )
    await user.click(screen.getByRole('tab', { name: 'Pricing groups' }))
    await user.click(
      screen.getByRole('button', { name: 'Save group settings' })
    )
    await waitFor(() =>
      expect(
        screen.getByRole('tab', { name: 'Auto group order' })
      ).toHaveAttribute('aria-selected', 'true')
    )
    const input = screen.getByRole('spinbutton', {
      name: 'Maximum custom groups per token',
    })
    expect(input).toHaveAttribute('aria-invalid', 'true')
    await waitFor(() => expect(input).toHaveFocus())
    expect(screen.getByText('Enter a positive integer')).toBeVisible()
    expect(onSave).not.toHaveBeenCalled()
  })

  it('shows empty state guidance and disables save during a pending request', async () => {
    render(
      <Fixture
        isSaving
        initial={{
          GroupRatio: '{}',
          TopupGroupRatio: '{}',
          AutoGroups: '[]',
        }}
      />
    )
    expect(
      screen.getByText('No groups yet. Add a group to get started.')
    ).toBeVisible()
    expect(screen.getByRole('button', { name: 'Saving...' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Add group' })).toBeDisabled()
  })

  it('finds notes case-insensitively and keeps a focused row until editing ends', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(
      <Fixture
        initial={{ GroupNotes: '{"vip":"Fallback","default":"Primary"}' }}
        onSave={onSave}
      />
    )
    await user.type(
      screen.getByRole('textbox', { name: 'Search groups...' }),
      'FALLBACK'
    )
    const note = screen.getByRole('textbox', { name: 'Internal note for vip' })
    await user.clear(note)
    await user.type(note, 'Edited')
    expect(note).toHaveFocus()
    expect(note).toHaveValue('Edited')
    await user.tab()
    expect(note).toBeVisible()
    await user.click(screen.getByRole('textbox', { name: 'Search groups...' }))
    expect(
      screen.queryByRole('textbox', { name: 'Internal note for vip' })
    ).not.toBeInTheDocument()
    await user.click(
      screen.getByRole('button', { name: 'Save group settings' })
    )
    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith({
        ...defaults,
        GroupNotes: '{"default":"Primary","vip":"Edited"}',
      })
    )
  })

  it('moves a note with a renamed row and removes it when that row is deleted', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(
      <Fixture
        initial={{ GroupNotes: '{"vip":"Fallback","default":"Primary"}' }}
        onSave={onSave}
      />
    )
    const names = screen.getAllByRole('textbox', { name: 'Group name' })
    fireEvent.change(names[1], { target: { value: 'default' } })
    await user.click(
      screen.getByRole('button', { name: 'Save group settings' })
    )
    expect(
      await screen.findByText('Enter a unique name for each pricing group')
    ).toBeVisible()
    expect(onSave).not.toHaveBeenCalled()
    fireEvent.change(names[1], { target: { value: 'renamed' } })
    expect(
      screen.getByRole('textbox', { name: 'Internal note for renamed' })
    ).toHaveValue('Fallback')
    await user.click(screen.getAllByRole('button', { name: 'Delete' })[0])
    await user.click(
      screen.getByRole('button', { name: 'Save group settings' })
    )
    await waitFor(() =>
      expect(onSave.mock.calls[0]?.[0].GroupNotes).toBe(
        '{"renamed":"Fallback"}'
      )
    )
  })

  it('keeps invalid JSON visible and restores edited notes in the visual editor when corrected', async () => {
    const user = userEvent.setup()
    render(<Fixture initial={{ GroupNotes: '{"vip":"initial"}' }} />)
    const note = screen.getByRole('textbox', { name: 'Internal note for vip' })
    fireEvent.change(note, { target: { value: 'draft' } })
    await user.click(screen.getByRole('button', { name: 'Switch to JSON' }))
    const editor = screen.getByRole('textbox', { name: 'Internal note' })
    expect(editor).toHaveValue('{"vip":"draft"}')
    fireEvent.input(editor, { target: { value: '{"vip":null}' } })
    await user.click(screen.getByRole('button', { name: 'Switch to Visual' }))
    expect(editor).toHaveValue('{"vip":null}')
    expect(
      screen.getByText(
        'Group notes must be a JSON object mapping group names to strings'
      )
    ).toBeVisible()
    fireEvent.input(editor, { target: { value: '{"vip":"changed"}' } })
    await user.click(screen.getByRole('button', { name: 'Switch to Visual' }))
    expect(
      screen.getByRole('textbox', { name: 'Internal note for vip' })
    ).toHaveValue('changed')
  })

  it('rejects a note over 200 Unicode code points without truncating the draft', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(<Fixture onSave={onSave} />)
    const note = screen.getByRole('textbox', { name: 'Internal note for vip' })
    const text = '🙂'.repeat(201)
    fireEvent.change(note, { target: { value: text } })
    await user.click(
      screen.getByRole('button', { name: 'Save group settings' })
    )
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Each group note must be at most 200 characters'
    )
    expect(note).toHaveValue(text)
    expect(onSave).not.toHaveBeenCalled()
  })

  it('preserves pricing edits when switching between visual and JSON editors', async () => {
    const user = userEvent.setup()
    render(<Fixture />)
    const vipRatio = screen.getAllByRole('spinbutton', { name: 'Ratio' })[1]
    await user.clear(vipRatio)
    await user.type(vipRatio, '0.5')
    await user.click(screen.getByRole('button', { name: 'Switch to JSON' }))
    await user.click(screen.getByRole('button', { name: 'Switch to Visual' }))
    expect(screen.getAllByRole('spinbutton', { name: 'Ratio' })[1]).toHaveValue(
      0.5
    )
  })
})
