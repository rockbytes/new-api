/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
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
