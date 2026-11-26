import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { getUsageExportEstimate, getUsageSummary } from '../api'
import { UsageReport } from '../index'

const membership = vi.hoisted(() => ({
  admin: false,
  error: false,
  refetch: vi.fn(),
}))

vi.mock('../api', () => ({
  getUsageSummary: vi.fn(async () => ({
    meta: { scope: { view: 'self' } },
    summary: {
      consume_quota: 0,
      requests: 0,
      prompt_tokens: 0,
      completion_tokens: 0,
    },
  })),
  getUsageTrend: vi.fn(async () => ({ points: [] })),
  getUsageModels: vi.fn(async () => ({ models: [] })),
  getUsageRankings: vi.fn(async () => ({ rank_by: '', rows: [] })),
  getUsageRecords: vi.fn(async () => ({
    meta: { scope: { view: 'self' } },
    items: [],
    total: 0,
  })),
  getUsageModelOptions: vi.fn(async () => ({ models: ['model-a'] })),
  getUsageExports: vi.fn(async () => []),
  getUsageExportEstimate: vi.fn(async () => ({
    delivery: 'queued',
    row_count: 1,
  })),
  createUsageExport: vi.fn(async () => ({})),
}))
vi.mock('@/features/organization-admin/hooks/use-organization-self', () => ({
  useOrganizationSelf: () => ({
    data: membership.admin ? { role: 'admin', status: 'active' } : null,
    isSuccess: !membership.error,
    isError: membership.error,
    refetch: membership.refetch,
  }),
}))
vi.mock('@/features/organizations/api', () => ({
  getOrganizations: vi.fn(async () => ({ items: [{ id: 3, name: 'Org A' }] })),
  getOrganizationDetail: vi.fn(async () => ({
    members: [{ user_id: 9, username: 'alice' }],
  })),
}))
vi.mock('@/features/organization-admin/api', () => ({
  getOrganizationMembers: vi.fn(async () => [
    { user_id: 9, username: 'alice' },
  ]),
}))

afterEach(() => {
  membership.admin = false
  membership.error = false
  useAuthStore.getState().auth.reset()
  vi.clearAllMocks()
})

describe('payer tabs', () => {
  it('offers export after the date picker and keeps export history below the report', async () => {
    useAuthStore
      .getState()
      .auth.setUser({ id: 5, username: 'user', role: 1, status: 1 })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <UsageReport />
      </QueryClientProvider>
    )

    const dateFilters = screen.getByRole('group', { name: 'Time range' })
    const button = within(dateFilters).getByRole('button', {
      name: 'Export usage summary',
    })
    expect(
      screen.getAllByRole('button', { name: 'Export usage summary' })
    ).toHaveLength(1)
    const history = screen.getByText('No exports yet')
    expect(
      screen
        .getByText('Records', { exact: true })
        .compareDocumentPosition(history) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()

    const datePicker = within(dateFilters).getByRole('button', {
      expanded: false,
    })
    const preset = within(dateFilters).getByRole('combobox', {
      name: 'Time range',
    })
    expect(preset).toHaveTextContent('Last 7 days')
    const user = userEvent.setup()
    preset.focus()
    await user.tab()
    expect(datePicker).toHaveFocus()
    await user.tab()
    expect(button).toHaveFocus()
    await user.keyboard('{Enter}')
    expect(
      await screen.findByRole('alertdialog', { name: 'Export usage summary' })
    ).toBeVisible()
  })

  it('keeps presets and custom dates available and exports the selected range', async () => {
    useAuthStore
      .getState()
      .auth.setUser({ id: 5, username: 'user', role: 1, status: 1 })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <UsageReport />
      </QueryClientProvider>
    )

    const user = userEvent.setup()
    const dateFilters = screen.getByRole('group', { name: 'Time range' })
    const preset = within(dateFilters).getByRole('combobox', {
      name: 'Time range',
    })
    await user.click(
      within(dateFilters).getByRole('button', { expanded: false })
    )
    const start = '2026-01-02T03:04:05'
    const end = '2026-01-03T06:07:08'
    fireEvent.change(screen.getByLabelText('Start Time'), {
      target: { value: start },
    })
    fireEvent.change(screen.getByLabelText('End Time'), {
      target: { value: end },
    })
    await user.click(screen.getByRole('button', { name: 'Confirm' }))

    expect(preset).toHaveTextContent('Custom')
    const bounds = {
      start_timestamp: new Date(start).getTime() / 1000,
      end_timestamp: new Date(end).getTime() / 1000,
    }
    await waitFor(() =>
      expect(getUsageSummary).toHaveBeenLastCalledWith(
        expect.objectContaining(bounds)
      )
    )
    await user.click(
      within(dateFilters).getByRole('button', { name: 'Export usage summary' })
    )
    await user.click(screen.getByRole('button', { name: 'Start export' }))
    await waitFor(() =>
      expect(getUsageExportEstimate).toHaveBeenLastCalledWith(
        expect.objectContaining(bounds)
      )
    )
    await waitFor(() =>
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    )

    await user.click(preset)
    expect(screen.getAllByRole('option')).toHaveLength(3)
    await user.click(screen.getByRole('option', { name: 'Last 90 days' }))
    expect(preset).toHaveTextContent('Last 90 days')
    await waitFor(() => {
      const [query] = vi.mocked(getUsageSummary).mock.lastCall ?? []
      expect(query && query.end_timestamp - query.start_timestamp).toBe(
        90 * 86400
      )
    })
  })

  it('blocks report and export when membership lookup fails and offers retry', async () => {
    membership.error = true
    useAuthStore
      .getState()
      .auth.setUser({ id: 5, username: 'user', role: 1, status: 1 })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <UsageReport />
      </QueryClientProvider>
    )
    expect(screen.getByRole('alert')).toHaveTextContent(
      'The usage report could not be loaded'
    )
    expect(getUsageSummary).not.toHaveBeenCalled()
    expect(
      screen.queryByRole('button', { name: 'Export usage summary' })
    ).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(membership.refetch).toHaveBeenCalled()
  })
  it.each([
    { role: 10, admin: false },
    { role: 1, admin: true },
  ])(
    'resets filters and exports the selected payer for role $role',
    async ({ role, admin }) => {
      membership.admin = admin
      useAuthStore
        .getState()
        .auth.setUser({ id: 5, username: 'user', role, status: 1 })
      const client = new QueryClient({
        defaultOptions: { queries: { retry: false } },
      })
      render(
        <QueryClientProvider client={client}>
          <UsageReport />
        </QueryClientProvider>
      )
      expect(
        screen.queryByPlaceholderText('Search organizations')
      ).not.toBeInTheDocument()
      await userEvent.click(screen.getByRole('tab', { name: 'Organization' }))
      if (!admin) {
        await userEvent.click(
          await screen.findByPlaceholderText('Search organizations')
        )
        await userEvent.click(
          await screen.findByRole('option', { name: 'Org A' })
        )
      }
      await userEvent.click(
        await screen.findByPlaceholderText('Search members')
      )
      await userEvent.click(
        await screen.findByRole('option', { name: 'alice' })
      )
      await userEvent.click(screen.getByPlaceholderText('Search models'))
      await userEvent.click(
        await screen.findByRole('option', { name: 'model-a' })
      )
      await waitFor(() =>
        expect(getUsageSummary).toHaveBeenCalledWith(
          expect.objectContaining({
            payer_type: 'organization',
            user_id: 9,
            model_name: 'model-a',
            ...(admin ? {} : { organization_id: 3 }),
          })
        )
      )
      await userEvent.click(
        screen.getByRole('button', { name: 'Export usage summary' })
      )
      await userEvent.click(
        screen.getByRole('button', { name: 'Start export' })
      )
      await waitFor(() =>
        expect(getUsageExportEstimate).toHaveBeenCalledWith(
          expect.objectContaining({
            payer_type: 'organization',
            user_id: 9,
            model_name: 'model-a',
          })
        )
      )
      await userEvent.click(screen.getByRole('tab', { name: 'Personal' }))
      expect(
        screen.queryByPlaceholderText('Search organizations')
      ).not.toBeInTheDocument()
      expect(
        screen.queryByPlaceholderText('Search members')
      ).not.toBeInTheDocument()
      await waitFor(() =>
        expect(getUsageSummary).toHaveBeenCalledWith(
          expect.objectContaining({
            payer_type: 'personal',
            organization_id: undefined,
            user_id: undefined,
            model_name: undefined,
            view: admin ? 'self' : 'platform',
          })
        )
      )
      await userEvent.click(
        screen.getByRole('button', { name: 'Export usage summary' })
      )
      await userEvent.click(
        screen.getByRole('button', { name: 'Start export' })
      )
      await waitFor(() =>
        expect(getUsageExportEstimate).toHaveBeenCalledWith(
          expect.objectContaining({
            payer_type: 'personal',
            organization_id: undefined,
            user_id: undefined,
            model_name: undefined,
          })
        )
      )
    }
  )
  it('keeps organization history available to ordinary users and isolates report requests', async () => {
    useAuthStore
      .getState()
      .auth.setUser({ id: 5, username: 'user', role: 1, status: 1 })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <UsageReport />
      </QueryClientProvider>
    )
    await waitFor(() =>
      expect(getUsageSummary).toHaveBeenCalledWith(
        expect.objectContaining({ view: 'self', payer_type: 'personal' })
      )
    )
    await userEvent.click(screen.getByRole('tab', { name: 'Organization' }))
    expect(screen.getByRole('tab', { name: 'Organization' })).toHaveAttribute(
      'aria-selected',
      'true'
    )
    await waitFor(() =>
      expect(getUsageSummary).toHaveBeenCalledWith(
        expect.objectContaining({
          payer_type: 'organization',
          user_id: undefined,
          organization_id: undefined,
        })
      )
    )
    expect(
      (await screen.findAllByText('No usage in this range')).length
    ).toBeGreaterThan(0)
    expect(screen.queryByPlaceholderText('Member')).not.toBeInTheDocument()
  })
})
