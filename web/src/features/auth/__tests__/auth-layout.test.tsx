import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'

import { AuthLayout } from '../auth-layout'

function renderAuthPage() {
  const root = createRootRoute({ component: Outlet })
  const signIn = createRoute({
    getParentRoute: () => root,
    path: '/sign-in',
    component: () => (
      <AuthLayout>
        <h2>Sign in to your account</h2>
      </AuthLayout>
    ),
  })
  const home = createRoute({
    getParentRoute: () => root,
    path: '/',
    component: () => <p>Home page</p>,
  })
  const router = createRouter({
    routeTree: root.addChildren([signIn, home]),
    history: createMemoryHistory({ initialEntries: ['/sign-in'] }),
  })
  render(
    <QueryClientProvider client={new QueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  return router
}

test('the sign-in form renders inside the auth card with a named brand link to home', async () => {
  renderAuthPage()
  expect(
    await screen.findByRole('heading', { name: 'Sign in to your account' })
  ).toBeVisible()
  expect(screen.getByRole('link', { name: 'AI Gateway' })).toHaveAttribute(
    'href',
    '/'
  )
})

test('the brand link returns to the home page', async () => {
  const router = renderAuthPage()
  await screen.findByRole('heading', { name: 'Sign in to your account' })
  screen.getByRole('link', { name: 'AI Gateway' }).click()
  await router.load()
  expect(await screen.findByText('Home page')).toBeVisible()
})
