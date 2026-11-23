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
import { expect, test, vi } from 'vitest'

import { SignIn } from '..'

vi.mock('@/hooks/use-status', () => ({
  useStatus: () => ({ status: { register_enabled: true } }),
}))

vi.mock('@tanstack/react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-router')>()),
  useSearch: () => ({ redirect: undefined }),
}))

function renderSignIn() {
  const root = createRootRoute({ component: Outlet })
  const signIn = createRoute({
    getParentRoute: () => root,
    path: '/sign-in',
    component: SignIn,
  })
  const signUp = createRoute({
    getParentRoute: () => root,
    path: '/sign-up',
    component: () => <p>Sign up page</p>,
  })
  const router = createRouter({
    routeTree: root.addChildren([signIn, signUp]),
    history: createMemoryHistory({ initialEntries: ['/sign-in'] }),
  })
  render(
    <QueryClientProvider client={new QueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

test('the sign-in page offers no sign-up entry even when registration is enabled', async () => {
  renderSignIn()

  expect(
    await screen.findByRole('heading', { name: 'Sign in' })
  ).toBeInTheDocument()
  expect(screen.queryByText("Don't have an account?")).not.toBeInTheDocument()
  expect(
    screen.queryByRole('link', { name: 'Sign up' })
  ).not.toBeInTheDocument()
})
