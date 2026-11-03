import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import { act, render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'

import { NotFoundError } from '../not-found-error'

function renderMissingPage() {
  const root = createRootRoute({ component: Outlet })
  const home = createRoute({
    getParentRoute: () => root,
    path: '/',
    component: () => <p>Home page</p>,
  })
  const missing = createRoute({
    getParentRoute: () => root,
    path: '/missing',
    component: NotFoundError,
  })
  const router = createRouter({
    routeTree: root.addChildren([home, missing]),
    history: createMemoryHistory({ initialEntries: ['/missing'] }),
  })
  render(<RouterProvider router={router} />)
  return router
}

test('an unknown address shows the 404 heading and a message', async () => {
  renderMissingPage()
  expect(await screen.findByRole('heading', { name: '404' })).toBeVisible()
  expect(screen.getByText('Oops! Page Not Found!')).toBeVisible()
})

test('the Back to Home action leaves the missing page for the home page', async () => {
  const router = renderMissingPage()
  await screen.findByRole('heading', { name: '404' })
  await act(() => screen.getByRole('button', { name: 'Back to Home' }).click())
  await act(() => router.load())
  expect(await screen.findByText('Home page')).toBeVisible()
})
