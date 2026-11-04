import { createFileRoute, redirect } from '@tanstack/react-router'

import { resolveAuthentication } from '@/lib/auth-session'
import { useAuthStore } from '@/stores/auth-store'

// The root path has no page of its own: signed-in users go straight to the console
// overview, and everyone else is sent to sign in.
export const Route = createFileRoute('/')({
  beforeLoad: async () => {
    await resolveAuthentication()

    const { auth } = useAuthStore.getState()

    if (auth.user && auth.accessToken) {
      throw redirect({
        to: '/dashboard/$section',
        params: { section: 'overview' },
      })
    }
    throw redirect({ to: '/sign-in' })
  },
})
