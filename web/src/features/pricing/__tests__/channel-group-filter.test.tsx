import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { STATUS_QUERY_KEY } from '@/lib/status-query'

import { Pricing } from '..'
import type { PricingModel } from '../types'

vi.mock('@visactor/react-vchart', () => ({ VChart: () => null }))

const model = (id: number, name: string, groups: string[]): PricingModel => ({
  id,
  model_name: name,
  quota_type: 0,
  model_ratio: 1,
  completion_ratio: 1,
  enable_groups: groups,
})

async function renderCatalog(
  initialEntry: string,
  groups: Record<string, string>
) {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => undefined)
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/pricing') {
      return {
        data: {
          success: true,
          data: [
            {
              ...model(1, 'common-model', [
                'standard',
                'premium',
                'premium',
                'internal',
                'retired',
              ]),
              supported_endpoint_types: ['openai', 'openai-response'],
              supported_endpoints_by_group: {
                standard: ['openai'],
                premium: ['openai-response'],
              },
            },
            model(2, 'standard-model', ['standard']),
            model(3, 'premium-model', ['premium']),
          ],
          vendors: [],
          usable_group: groups,
          group_ratio: { standard: 1, premium: 2, empty: 1 },
          auto_groups: ['standard'],
        },
      }
    }
    if (url === '/api/notice') return { data: { success: true, data: '' } }
    return { data: { success: true, data: { models: [] } } }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  client.setQueryData(STATUS_QUERY_KEY, {})
  const root = createRootRoute()
  const pricing = createRoute({
    getParentRoute: () => root,
    path: '/pricing/',
    component: Pricing,
    validateSearch: (search: Record<string, unknown>) => search,
  })
  const router = createRouter({
    routeTree: root.addChildren([pricing]),
    history: createMemoryHistory({ initialEntries: [initialEntry] }),
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  await screen.findByRole('heading', { name: 'common-model' })
}

it('filters by a usable channel group and restores the deduplicated union', async () => {
  const user = userEvent.setup()
  await renderCatalog('/pricing/', {
    standard: '',
    premium: '',
    empty: '',
    auto: '',
    retired: '',
  })
  const all = screen.getByRole('button', { name: /^All Groups\s*3$/ })
  const premium = screen.getByRole('button', { name: /^premium\s*2$/ })
  expect(all).toHaveAttribute('aria-pressed', 'true')
  expect(screen.getByRole('button', { name: /^standard\s*2$/ })).toBeVisible()
  expect(
    screen.queryByRole('button', { name: /^(internal|empty|auto|retired)/ })
  ).toBeNull()

  premium.focus()
  await user.keyboard('{Enter}')
  expect(premium).toHaveAttribute('aria-pressed', 'true')
  expect(screen.queryByRole('heading', { name: 'standard-model' })).toBeNull()
  expect(screen.getByRole('heading', { name: 'premium-model' })).toBeVisible()
  expect(screen.getAllByRole('heading', { name: 'common-model' })).toHaveLength(
    1
  )

  await user.click(all)
  expect(all).toHaveAttribute('aria-pressed', 'true')
  expect(screen.getByRole('heading', { name: 'standard-model' })).toBeVisible()
})

it('uses the selected group for endpoint counts, card labels, and combined filtering', async () => {
  const user = userEvent.setup()
  await renderCatalog('/pricing/', { standard: '', premium: '' })
  const sidebar = within(screen.getByRole('complementary'))
  const catalog = within(screen.getByRole('main'))
  expect(sidebar.getByRole('button', { name: /^Response\s*1$/ })).toBeVisible()

  await user.click(sidebar.getByRole('button', { name: /^standard\s*2$/ }))
  expect(sidebar.getByRole('button', { name: /^Response\s*0$/ })).toBeVisible()
  expect(catalog.getByTitle('openai')).toBeVisible()
  expect(catalog.queryByTitle(/openai-response/)).not.toBeInTheDocument()

  await user.click(sidebar.getByRole('button', { name: /^Response\s*0$/ }))
  expect(
    catalog.queryByRole('heading', { name: 'common-model' })
  ).not.toBeInTheDocument()
  expect(
    catalog.getByRole('heading', { name: 'No models found' })
  ).toBeVisible()
})

it('shows the selected group endpoints in the table view', async () => {
  await renderCatalog('/pricing/?group=standard', { standard: '', premium: '' })
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Table view' }))
  const row = within(screen.getByRole('row', { name: /common-model/ }))
  expect(row.getByText('openai', { exact: true })).toBeVisible()
  expect(
    row.queryByText('openai-response', { exact: true })
  ).not.toBeInTheDocument()
})

it('opens model details with only authorized groups and selected-group endpoints', async () => {
  const user = userEvent.setup()
  await renderCatalog('/pricing/', { standard: '', premium: '' })
  await user.click(screen.getByRole('button', { name: /^standard\s*2$/ }))
  const modelCard = screen
    .getByRole('heading', { name: 'common-model' })
    .closest('[data-slot="card"]')
  expect(modelCard).not.toBeNull()
  await user.click(
    within(modelCard as HTMLElement).getByRole('button', { name: /Details/ })
  )
  const details = within(
    await screen.findByRole('dialog', { name: 'common-model' })
  )
  expect(details.getByText('openai', { exact: true })).toBeVisible()
  expect(
    details.queryByText('openai-response', { exact: true })
  ).not.toBeInTheDocument()
  expect(
    details.queryByText('internal', { exact: true })
  ).not.toBeInTheDocument()
  expect(
    details.queryByText('retired', { exact: true })
  ).not.toBeInTheDocument()
})

it('shows only channel group and endpoint filters on desktop and in the mobile drawer', async () => {
  const user = userEvent.setup()
  await renderCatalog('/pricing/', { standard: '', premium: '' })

  const sidebar = within(screen.getByRole('complementary'))
  expect(sidebar.getByRole('button', { name: 'Channel Groups' })).toBeVisible()
  expect(sidebar.getByRole('button', { name: 'Endpoint Type' })).toBeVisible()
  expect(
    sidebar.queryAllByRole('button', {
      name: /^(All Vendors|Model Tags|Pricing Type)$/,
    })
  ).toHaveLength(0)

  await user.click(screen.getByRole('button', { name: 'Filter' }))
  const drawer = within(await screen.findByRole('dialog', { name: 'Filter' }))
  expect(drawer.getByRole('button', { name: 'Channel Groups' })).toBeVisible()
  expect(drawer.getByRole('button', { name: 'Endpoint Type' })).toBeVisible()
  expect(
    drawer.queryAllByRole('button', {
      name: /^(All Vendors|Model Tags|Pricing Type)$/,
    })
  ).toHaveLength(0)
})

it('ignores old vendor, tag and pricing type URL filters', async () => {
  await renderCatalog(
    '/pricing/?vendor=missing&tag=missing&quotaType=request',
    {
      standard: '',
      premium: '',
    }
  )
  expect(screen.getByRole('button', { name: 'Reset' })).toBeDisabled()
  expect(screen.getByRole('heading', { name: 'standard-model' })).toBeVisible()
  expect(screen.getByRole('heading', { name: 'premium-model' })).toBeVisible()
})

it('ignores a stale group from the URL when the viewer cannot select it', async () => {
  await renderCatalog('/pricing/?group=internal', { standard: '', premium: '' })
  expect(
    screen.getByRole('button', { name: /^All Groups\s*3$/ })
  ).toHaveAttribute('aria-pressed', 'true')
  expect(screen.getByRole('heading', { name: 'standard-model' })).toBeVisible()
  expect(screen.getByRole('heading', { name: 'premium-model' })).toBeVisible()
})
