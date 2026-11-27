import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import {
  render,
  screen,
  waitFor,
  within,
  fireEvent,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { TooltipProvider } from '@/components/ui/tooltip'
import { api } from '@/lib/api'
import { Route as UsersRoute } from '@/routes/_authenticated/users/index'
import { useAuthStore } from '@/stores/auth-store'

import { Users } from '../..'
import type { UserRateLimit } from '../../types'

const clients: QueryClient[] = []
const premiumSearchUrl =
  '/api/user/search?keyword=&group=premium&p=1&page_size=20'

beforeEach(() => {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  localStorage.clear()
  useAuthStore.getState().auth.setUser({ id: 1, username: 'admin', role: 100 })
})

afterEach(() => {
  clients.splice(0).forEach((client) => client.clear())
  localStorage.clear()
  useAuthStore.getState().auth.reset()
})

async function renderUsers(
  initialEntry = '/users/',
  limits?: UserRateLimit,
  targetRole = 1
) {
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/user/2/rate_limit') {
      return {
        data: {
          success: !!limits,
          data: limits,
          message: 'Could not read limits',
        },
      }
    }
    if (url === '/api/group/') {
      return { data: { success: true, data: ['default', 'premium'] } }
    }
    return {
      data: {
        success: true,
        data: {
          items: [
            {
              id: 2,
              username: 'premium-user',
              display_name: '',
              role: targetRole,
              status: 1,
              quota: 0,
              used_quota: 0,
              request_count: 0,
              group: 'premium',
            },
          ],
          total: 1,
        },
      },
    }
  })
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const users = createRoute({
    getParentRoute: () => auth,
    path: 'users/',
    validateSearch: UsersRoute.options.validateSearch,
    component: () => (
      <TooltipProvider>
        <Users />
      </TooltipProvider>
    ),
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([users])]),
    history: createMemoryHistory({ initialEntries: [initialEntry] }),
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  await router.load()
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  await screen.findByText('premium-user')
  return { get, router }
}

// The User Group column header is a sort menu with the same name; the
// filter chip is the one that opens a dialog.
function getGroupFilterTrigger() {
  const trigger = screen
    .getAllByRole('button', { name: /^User Group/ })
    .find((button) => button.getAttribute('aria-haspopup') === 'dialog')
  expect(trigger).toBeDefined()
  return trigger as HTMLElement
}

it('searches only the selected user group and lists all users again after the filter is cleared', async () => {
  const user = userEvent.setup()
  const { get, router } = await renderUsers()
  expect(get).toHaveBeenCalledWith('/api/user/', {
    params: { p: 1, page_size: 20 },
  })

  await user.click(getGroupFilterTrigger())
  await user.click(await screen.findByRole('option', { name: /^premium/ }))

  await waitFor(() => expect(get).toHaveBeenLastCalledWith(premiumSearchUrl))
  expect(router.state.location.search).toMatchObject({ group: ['premium'] })
  expect(getGroupFilterTrigger()).toHaveTextContent('premium')

  await user.click(screen.getByRole('option', { name: 'Clear filters' }))

  await waitFor(() =>
    expect(get).toHaveBeenLastCalledWith('/api/user/', {
      params: { p: 1, page_size: 20 },
    })
  )
  expect(router.state.location.search).not.toHaveProperty('group')
})

it('restores the user group filter from the page URL on load', async () => {
  const { get } = await renderUsers('/users/?group=%5B%22premium%22%5D')

  expect(get).toHaveBeenCalledWith(premiumSearchUrl)
  expect(get).not.toHaveBeenCalledWith('/api/user/', expect.anything())
  expect(getGroupFilterTrigger()).toHaveTextContent('premium')
})

const overrideSearchUrl =
  '/api/user/search?keyword=&group=&has_rate_limit_override=true&p=1&page_size=20'
const userLimits = {
  enabled: true,
  duration_minutes: 60,
  defaults: { total: 700, success: 0 },
  override: null,
}

async function openRateLimitDialog(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: 'Open menu' }))
  await user.click(
    screen.getByRole('menuitem', { name: 'Configure rate limit' })
  )
  return screen.findByRole('dialog', { name: 'Configure rate limit' })
}

it('searches for users with custom limits, then omits the parameter when cleared', async () => {
  const user = userEvent.setup()
  const { get, router } = await renderUsers()
  expect(get).not.toHaveBeenCalledWith(
    '/api/user/2/rate_limit',
    expect.anything()
  )

  await user.click(screen.getByRole('button', { name: 'Rate limits' }))
  await user.click(screen.getByRole('option', { name: /^Has custom limits/ }))

  await waitFor(() => expect(get).toHaveBeenLastCalledWith(overrideSearchUrl))
  expect(router.state.location.search).toMatchObject({
    has_rate_limit_override: true,
  })
  expect(
    screen.queryByRole('columnheader', { name: 'Rate limits' })
  ).not.toBeInTheDocument()

  await user.click(screen.getByRole('option', { name: 'Clear filters' }))
  await waitFor(() =>
    expect(get).toHaveBeenLastCalledWith('/api/user/', {
      params: { p: 1, page_size: 20 },
    })
  )
  expect(router.state.location.search).not.toHaveProperty(
    'has_rate_limit_override'
  )
})

it('prefills custom limits from read-only defaults, saves zero and refreshes the users list', async () => {
  const user = userEvent.setup()
  const { get } = await renderUsers('/users/', userLimits)
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  const dialog = await openRateLimitDialog(user)
  const scope = within(dialog)
  const inherit = await scope.findByRole('radio', { name: 'Inherit defaults' })
  expect(inherit).toBeChecked()
  expect(
    scope.getByRole('region', { name: 'Default limits per user' })
  ).toHaveTextContent('Unlimited')
  expect(scope.queryByRole('spinbutton')).not.toBeInTheDocument()
  expect(scope.getByText('Limit period: 60 min')).toBeInTheDocument()

  inherit.focus()
  await user.keyboard('{ArrowDown}')
  expect(scope.getByRole('radio', { name: 'Custom limits' })).toBeChecked()
  const total = scope.getByRole('spinbutton', {
    name: 'Max Requests (including failures)',
  })
  expect(total).toHaveValue(700)
  expect(
    scope.getByRole('spinbutton', { name: 'Max Successful Requests' })
  ).toHaveValue(0)
  fireEvent.change(total, { target: { value: '0' } })
  await user.click(scope.getByRole('button', { name: 'Save' }))

  await waitFor(() =>
    expect(put).toHaveBeenCalledWith('/api/user/2/rate_limit', {
      override: { total: 0, success: 0 },
    })
  )
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  expect(get.mock.calls.filter(([url]) => url === '/api/user/')).toHaveLength(2)
})

it('uses null to restore inheritance and keeps the dialog open if saving fails', async () => {
  const user = userEvent.setup()
  const { get } = await renderUsers('/users/?has_rate_limit_override=true', {
    ...userLimits,
    enabled: false,
    override: { total: 0, success: 2 },
  })
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValueOnce({
      data: { success: false, message: 'Rate limit update refused' },
    })
    .mockResolvedValue({ data: { success: true } })
  const dialog = await openRateLimitDialog(user)
  const scope = within(dialog)
  expect(
    await scope.findByRole('radio', { name: 'Custom limits' })
  ).toBeChecked()
  expect(scope.getByRole('alert')).toHaveTextContent(
    'Saved limits are not enforced'
  )
  await user.click(scope.getByRole('radio', { name: 'Inherit defaults' }))
  await user.click(scope.getByRole('button', { name: 'Save' }))
  expect(await scope.findByText('Rate limit update refused')).toBeVisible()
  expect(scope.getByRole('radio', { name: 'Inherit defaults' })).toBeChecked()
  expect(put).toHaveBeenLastCalledWith('/api/user/2/rate_limit', {
    override: null,
  })

  await user.click(scope.getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  expect(
    get.mock.calls.filter(([url]) => url === overrideSearchUrl)
  ).toHaveLength(2)
})

it('keeps save unavailable after a load failure and lets the administrator retry', async () => {
  const user = userEvent.setup()
  const { get } = await renderUsers()
  const dialog = await openRateLimitDialog(user)
  const scope = within(dialog)
  expect(await scope.findByText('Could not read limits')).toBeVisible()
  expect(scope.getByRole('button', { name: 'Save' })).toBeDisabled()
  get.mockResolvedValueOnce({ data: { success: true, data: userLimits } })
  await user.click(scope.getByRole('button', { name: 'Retry' }))
  expect(
    await scope.findByRole('radio', { name: 'Inherit defaults' })
  ).toBeChecked()
})

it('rejects an out-of-range custom count instead of truncating or clearing it', async () => {
  const user = userEvent.setup()
  await renderUsers('/users/', {
    ...userLimits,
    override: { total: 20, success: 0 },
  })
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  const dialog = await openRateLimitDialog(user)
  const scope = within(dialog)
  const total = await scope.findByRole('spinbutton', {
    name: 'Max Requests (including failures)',
  })
  fireEvent.change(total, { target: { value: '2147483648' } })
  await user.click(scope.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(total).toHaveAttribute('aria-invalid', 'true'))
  expect(put).not.toHaveBeenCalled()
})

it('disables configuring a peer administrator without making an API request', async () => {
  const user = userEvent.setup()
  useAuthStore.getState().auth.setUser({ id: 1, username: 'admin', role: 10 })
  const { get } = await renderUsers('/users/', userLimits, 10)

  await user.click(screen.getByRole('button', { name: 'Open menu' }))
  expect(
    screen.getByRole('menuitem', { name: 'Configure rate limit' })
  ).toHaveAttribute('aria-disabled', 'true')
  expect(get).not.toHaveBeenCalledWith(
    '/api/user/2/rate_limit',
    expect.anything()
  )
})
