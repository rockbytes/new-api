import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import {
  act,
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { OverviewDashboard } from '../overview-dashboard'

const storageKey = 'dashboard_overview_setup_guide_expanded'
const accountStatusKey = ['dashboard', 'overview', 'account-status']
const accountStatusPath = '/api/data/self/account-status'
const organizationA = {
  id: 7,
  name: 'Organization A',
  remaining_quota: 2_500_000,
  recent_usage: 500_000,
}
const organizationB = {
  id: 8,
  name: 'Organization B',
  remaining_quota: 4_000_000,
  recent_usage: 0,
}
let loadAccountStatus: () => Promise<unknown>
let client: QueryClient
let keyLookupError: Error | null

beforeEach(() => {
  window.localStorage.clear()
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'dashboard-user',
    role: 1,
    quota: 1000000,
    used_quota: 1000,
    request_count: 1,
  })
  client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  keyLookupError = null
  loadAccountStatus = async () => ({
    personal_usage: 1_500_000,
    organization: null,
  })
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    switch (url) {
      case '/api/token/?p=1&size=10':
        if (keyLookupError) throw keyLookupError
        return {
          data: {
            success: true,
            data: {
              items: [{ id: 1, name: 'App key', key: 'masked', status: 1 }],
            },
          },
        }
      case '/api/status':
        return {
          data: {
            data: {
              api_info_enabled: false,
              announcements_enabled: false,
              faq_enabled: false,
              uptime_kuma_enabled: false,
            },
          },
        }
      case '/api/user/models':
        return { data: { success: true, data: ['gpt-4o-mini'] } }
      case accountStatusPath:
        return { data: { success: true, data: await loadAccountStatus() } }
      default:
        throw new Error(`Unexpected dashboard request: ${url}`)
    }
  })
})

afterEach(() => {
  cleanup()
  client.clear()
  useAuthStore.setState(useAuthStore.getInitialState(), true)
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  window.localStorage.clear()
})

async function renderOverview() {
  const router = createRouter({
    routeTree: createRootRoute({ component: OverviewDashboard }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

describe('overview account status', () => {
  it('keeps both sections and the report shortcut without a current organization', async () => {
    await renderOverview()

    const card = within(screen.getByRole('region', { name: 'Account status' }))
    const personal = within(card.getByRole('region', { name: 'Personal' }))
    const organization = within(
      card.getByRole('region', { name: 'Organization' })
    )
    expect(await personal.findByText('$3')).toBeVisible()
    expect(personal.getByText('$2')).toBeVisible()
    expect(await organization.findByText('No organization')).toBeVisible()
    expect(organization.getAllByText('-', { exact: true })).toHaveLength(2)
    expect(card.queryByRole('link', { name: 'Wallet' })).not.toBeInTheDocument()
    expect(
      card.getByRole('link', { name: 'View usage report' })
    ).toHaveAttribute('href', '/usage-report')
  })

  it('shows only the numbers of the current organization, including real zero', async () => {
    loadAccountStatus = async () => ({
      personal_usage: 1_500_000,
      organization: organizationA,
    })
    await renderOverview()

    const organization = within(
      screen.getByRole('region', { name: 'Organization' })
    )
    expect(await organization.findByText('Organization A')).toBeVisible()
    expect(
      organization.getAllByRole('definition').map((value) => value.textContent)
    ).toEqual(['$5', '$1'])

    loadAccountStatus = async () => ({
      personal_usage: 1_500_000,
      organization: organizationB,
    })
    await act(async () => {
      await client.invalidateQueries({ queryKey: accountStatusKey })
    })

    expect(await organization.findByText('Organization B')).toBeVisible()
    expect(organization.queryByText('Organization A')).not.toBeInTheDocument()
    expect(
      organization.getAllByRole('definition').map((value) => value.textContent)
    ).toEqual(['$8', '$0'])
  })

  it('clears both organization numbers on exit and ignores a late old snapshot', async () => {
    loadAccountStatus = async () => ({
      personal_usage: 1_500_000,
      organization: organizationA,
    })
    await renderOverview()
    const organization = within(
      screen.getByRole('region', { name: 'Organization' })
    )
    expect(await organization.findByText('Organization A')).toBeVisible()

    let resolveOldSnapshot: (value: unknown) => void = () => undefined
    const oldSnapshot = new Promise((resolve) => {
      resolveOldSnapshot = resolve
    })
    const loadOldSnapshot = vi.fn(() => oldSnapshot)
    loadAccountStatus = loadOldSnapshot
    let olderRefresh: Promise<void> | undefined
    await act(async () => {
      olderRefresh = client.invalidateQueries({ queryKey: accountStatusKey })
    })
    expect(loadOldSnapshot).toHaveBeenCalled()
    await waitFor(() =>
      expect(organization.queryByText('Organization A')).not.toBeInTheDocument()
    )

    loadAccountStatus = async () => ({
      personal_usage: 1_500_000,
      organization: null,
    })
    await act(async () => {
      await client.invalidateQueries({ queryKey: accountStatusKey })
    })
    expect(await organization.findByText('No organization')).toBeVisible()

    await act(async () => {
      resolveOldSnapshot({
        personal_usage: 1_500_000,
        organization: organizationA,
      })
      await olderRefresh
    })
    expect(organization.getAllByText('-', { exact: true })).toHaveLength(2)
    expect(organization.queryByText('Organization A')).not.toBeInTheDocument()
  })

  it('shows dashes for both organization numbers if membership cannot be confirmed', async () => {
    loadAccountStatus = async () => ({
      personal_usage: 1_500_000,
      organization: null,
      organization_unavailable: true,
    })
    await renderOverview()

    const organization = within(
      screen.getByRole('region', { name: 'Organization' })
    )
    expect(await organization.findAllByText('-', { exact: true })).toHaveLength(
      2
    )
    expect(organization.queryByText('No organization')).not.toBeInTheDocument()
    expect(
      within(screen.getByRole('region', { name: 'Personal' })).getByText('$3')
    ).toBeVisible()
  })

  it('shows dashes for all three fetched metrics when the account status request fails', async () => {
    loadAccountStatus = async () => {
      throw new Error('Account status request failed')
    }
    await renderOverview()

    const personal = within(screen.getByRole('region', { name: 'Personal' }))
    const organization = within(
      screen.getByRole('region', { name: 'Organization' })
    )
    expect(await personal.findByText('-', { exact: true })).toBeVisible()
    expect(await organization.findAllByText('-', { exact: true })).toHaveLength(
      2
    )
    expect(personal.getByText('$2')).toBeVisible()
    expect(organization.queryByText('No organization')).not.toBeInTheDocument()
  })

  it('does not offer the report shortcut when that module is hidden', async () => {
    client.setQueryData(['status'], {
      SidebarModulesAdmin: JSON.stringify({
        console: { enabled: true, usage_report: false },
      }),
    })
    await renderOverview()

    const card = within(screen.getByRole('region', { name: 'Account status' }))
    expect(card.getByText('Personal balance')).toBeVisible()
    expect(
      card.queryByRole('link', { name: 'View usage report' })
    ).not.toBeInTheDocument()
  })

  it('shows dashes for missing usage without replacing the available balances', async () => {
    loadAccountStatus = async () => ({
      personal_usage: null,
      organization: { ...organizationA, recent_usage: null },
    })
    await renderOverview()

    const personal = within(screen.getByRole('region', { name: 'Personal' }))
    const organization = within(
      screen.getByRole('region', { name: 'Organization' })
    )
    expect(await personal.findByText('-', { exact: true })).toBeVisible()
    expect(await organization.findByText('-', { exact: true })).toBeVisible()
    expect(personal.getByText('$2')).toBeVisible()
    expect(organization.getByText('$5')).toBeVisible()
  })
})

describe('overview setup guide', () => {
  it('shows usage first and only a header entry when setup is complete', async () => {
    await renderOverview()

    const toggle = await screen.findByRole('button', { name: 'Setup guide' })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(
      screen.getAllByRole('heading').map((heading) => heading.textContent)
    ).toEqual(['Overview', 'Account status', 'Personal', 'Organization'])
    expect(screen.queryByText('Setup guide complete')).not.toBeInTheDocument()
    expect(screen.queryByText('Setup progress: 3/3')).not.toBeInTheDocument()
    for (const name of ['API Keys', 'Channels', 'Usage Logs', 'Pricing']) {
      expect(screen.queryByRole('button', { name })).not.toBeInTheDocument()
    }
    const panel = document.getElementById(
      toggle.getAttribute('aria-controls') ?? ''
    )
    expect(panel).toBeInTheDocument()
    expect(panel).not.toBeVisible()
  })

  it('toggles the completed guide with the keyboard and restores focus after hiding it', async () => {
    const user = userEvent.setup()
    await renderOverview()
    const toggle = await screen.findByRole('button', { name: 'Setup guide' })

    await user.tab()
    expect(toggle).toHaveFocus()
    await user.keyboard('{Enter}')
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(
      document.getElementById(toggle.getAttribute('aria-controls') ?? '')
    ).toBeVisible()
    expect(
      screen.getByRole('heading', {
        name: 'Build on your API gateway in minutes',
      })
    ).toBeVisible()
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /^API Keys/ })).toBeVisible()
    )

    await user.keyboard(' ')
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(toggle).toHaveFocus()
    await user.click(toggle)
    await user.click(screen.getByRole('button', { name: 'Hide setup guide' }))
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(toggle).toHaveFocus()
  })

  it('restores the completed guide preference after remounting', async () => {
    const user = userEvent.setup()
    const first = await renderOverview()
    await user.click(await screen.findByRole('button', { name: 'Setup guide' }))
    first.unmount()

    const second = await renderOverview()
    expect(
      await screen.findByRole('button', { name: 'Setup guide' })
    ).toHaveAttribute('aria-expanded', 'true')
    await user.click(screen.getByRole('button', { name: 'Hide setup guide' }))
    second.unmount()

    await renderOverview()
    expect(
      await screen.findByRole('button', { name: 'Setup guide' })
    ).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('Setup guide complete')).not.toBeInTheDocument()
  })

  it('keeps the existing progress banner when an incomplete guide is manually collapsed', async () => {
    const user = userEvent.setup()
    useAuthStore
      .getState()
      .auth.setUser({ id: 1, username: 'new-user', role: 1 })
    await renderOverview()

    await user.click(
      await screen.findByRole('button', { name: 'Hide setup guide' })
    )
    expect(screen.getByText('Setup progress: 1/3')).toBeVisible()
    expect(
      screen.getByText('Setup guide is collapsed. Expand it anytime.')
    ).toBeVisible()
    expect(screen.getByRole('button', { name: 'API Keys' })).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'Setup guide' })
    ).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Show setup guide' }))
    expect(
      screen.getByRole('button', { name: 'Hide setup guide' })
    ).toBeVisible()
  })

  it('removes the collapsed progress banner when the remaining setup step completes', async () => {
    useAuthStore.getState().auth.setUser({
      id: 1,
      username: 'dashboard-user',
      role: 1,
      quota: 1000000,
    })
    window.localStorage.setItem(storageKey, 'collapsed')
    await renderOverview()
    expect(await screen.findByText('Setup progress: 2/3')).toBeVisible()

    act(() => {
      useAuthStore.getState().auth.setUser({
        id: 1,
        username: 'dashboard-user',
        role: 1,
        quota: 1000000,
        request_count: 1,
      })
    })
    expect(
      await screen.findByRole('button', { name: 'Setup guide' })
    ).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText(/Setup progress:/)).not.toBeInTheDocument()
  })

  it('does not show a completed setup entry when the key lookup fails', async () => {
    keyLookupError = new Error('Key lookup unavailable')
    await renderOverview()

    expect(
      await screen.findByRole('button', { name: 'Hide setup guide' })
    ).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'Setup guide' })
    ).not.toBeInTheDocument()
  })
})
