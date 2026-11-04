import { afterEach, describe, expect, it } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { Route } from '../index'

type RedirectOptions = { to?: string; params?: Record<string, string> }

async function redirectOptionsFromRoot(): Promise<RedirectOptions> {
  try {
    await Route.options.beforeLoad?.({ location: {} } as never)
  } catch (error) {
    return (error as { options: RedirectOptions }).options
  }
  throw new Error('the root path should redirect')
}

afterEach(() => {
  useAuthStore.setState(useAuthStore.getInitialState(), true)
  window.localStorage.clear()
})

describe('root path redirect', () => {
  it('sends signed-out visitors to sign in', async () => {
    const options = await redirectOptionsFromRoot()

    expect(options.to).toBe('/sign-in')
  })

  it('sends signed-in users straight to the console overview', async () => {
    useAuthStore.setState((state) => ({
      ...state,
      auth: {
        ...state.auth,
        user: { id: 1, username: 'alice', role: 1 },
        accessToken: 'test-access-token',
        accessExpiresAt: Math.floor(Date.now() / 1000) + 3600,
        session: { id: 'test-session' } as never,
      },
    }))

    const options = await redirectOptionsFromRoot()

    expect(options.to).toBe('/dashboard/$section')
    expect(options.params).toEqual({ section: 'overview' })
  })
})
