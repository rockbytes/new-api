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
import { beforeEach, expect, test, vi } from 'vitest'

import { SignIn } from '..'

const statusState = vi.hoisted(() => ({
  status: {} as Record<string, unknown>,
}))

vi.mock('@/hooks/use-status', () => ({
  useStatus: () => ({ status: statusState.status }),
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

beforeEach(() => {
  statusState.status = {}
})

test('the sign-in page links to sign-up when registration is enabled', async () => {
  statusState.status = { register_enabled: true }
  renderSignIn()

  expect(
    await screen.findByRole('heading', { name: 'Sign in' })
  ).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Sign up' })).toHaveAttribute(
    'href',
    '/sign-up'
  )
})

test('the sign-in page offers no sign-up link when registration is disabled', async () => {
  statusState.status = { register_enabled: false }
  renderSignIn()

  expect(
    await screen.findByRole('heading', { name: 'Sign in' })
  ).toBeInTheDocument()
  expect(
    screen.queryByRole('link', { name: 'Sign up' })
  ).not.toBeInTheDocument()
})

test('the sign-in page offers no sign-up link in self-use mode even when registration is enabled', async () => {
  statusState.status = { register_enabled: true, self_use_mode_enabled: true }
  renderSignIn()

  expect(
    await screen.findByRole('heading', { name: 'Sign in' })
  ).toBeInTheDocument()
  expect(
    screen.queryByRole('link', { name: 'Sign up' })
  ).not.toBeInTheDocument()
})
