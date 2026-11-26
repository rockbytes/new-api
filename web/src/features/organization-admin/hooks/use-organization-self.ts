import { useQuery } from '@tanstack/react-query'

import {
  getServerErrorSources,
  getServerErrorStatus,
} from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { getOrganizationSelf } from '../api'

/**
 * The caller's organization membership. Accounts outside any organization get
 * a 404 from the self endpoint; callers treat that as "no membership" and the
 * query is not retried.
 */
export function useOrganizationSelf(enabled = true) {
  const userId = useAuthStore((state) => state.auth.user?.id)

  return useQuery({
    queryKey: ['organization-self', userId],
    queryFn: async () => {
      try {
        return await getOrganizationSelf()
      } catch (error) {
        if (
          getServerErrorStatus(error) === 404 &&
          getServerErrorSources(error).some(
            (source) => source.code === 'ORGANIZATION_MEMBERSHIP_NOT_FOUND'
          )
        ) {
          return null
        }
        throw error
      }
    },
    enabled: enabled && userId !== undefined,
    retry: false,
    staleTime: 60_000,
  })
}
