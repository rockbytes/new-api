import { useQuery } from '@tanstack/react-query'

import { useAuthStore } from '@/stores/auth-store'

import { getOrganizationSelf } from '../api'

/**
 * The caller's organization membership. Accounts outside any organization get
 * a 404 from the self endpoint; callers treat that as "no membership" and the
 * query is not retried.
 */
export function useOrganizationSelf() {
  const userId = useAuthStore((state) => state.auth.user?.id)

  return useQuery({
    queryKey: ['organization-self', userId],
    queryFn: getOrganizationSelf,
    enabled: userId !== undefined,
    retry: false,
    staleTime: 60_000,
  })
}
