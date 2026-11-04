import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'

import { STATUS_QUERY_KEY } from '@/lib/status-query'

import { SystemBrand } from '../system-brand'

it('links the console header logo to the console overview', async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(STATUS_QUERY_KEY, { system_name: 'Fumino Model Link' })

  const rootRoute = createRootRoute({
    component: () => <SystemBrand variant='inline' />,
  })
  const dashboardRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/dashboard/$section',
    component: () => null,
  })
  const router = createRouter({
    routeTree: rootRoute.addChildren([dashboardRoute]),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()

  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )

  const logoLink = await screen.findByRole('link', { name: 'Go to console' })
  expect(logoLink).toHaveAttribute('href', '/dashboard/overview')
})
