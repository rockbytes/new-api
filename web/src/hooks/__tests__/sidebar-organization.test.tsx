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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { NavGroup } from '@/components/layout/types'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { useSidebarView } from '../use-sidebar-view'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    useLocation: (options?: {
      select?: (location: { pathname: string }) => unknown
    }) => {
      const location = { pathname: '/dashboard/overview' }
      return options?.select ? options.select(location) : location
    },
  }
})

beforeEach(() => {
  vi.stubGlobal('localStorage', {
    getItem: () => null,
    setItem: () => undefined,
    removeItem: () => undefined,
  })
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  useAuthStore.getState().auth.reset()
})

function sidebarAs(
  role: number,
  organization: {
    id: number
    role: 'admin' | 'member'
    status: string
  } | null = null,
  adminConfig?: object
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(
    ['status'],
    adminConfig ? { SidebarModulesAdmin: JSON.stringify(adminConfig) } : {}
  )
  useAuthStore.getState().auth.setUser({
    id: 7,
    username: 'tester',
    role,
    permissions: { sidebar_settings: false },
    sidebar_modules: '',
    organization,
  })
  function Wrapper(props: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        {props.children}
      </QueryClientProvider>
    )
  }
  return renderHook(() => useSidebarView(), { wrapper: Wrapper })
}

function groupTitles(groups: NavGroup[]) {
  return Object.fromEntries(
    groups.map((group) => [group.id, group.items.map((item) => item.title)])
  )
}

describe('organization sidebar group', () => {
  it('shows the platform organizations entry to platform administrators', () => {
    const { result } = sidebarAs(ROLE.ADMIN)
    const groups = groupTitles(result.current.navGroups)
    expect(groups.organization).toEqual(['Company Organizations'])
    expect(groups.admin).not.toContain('Organizations')
  })

  it('shows the same platform organizations entry to root', () => {
    const { result } = sidebarAs(ROLE.SUPER_ADMIN)
    const groups = groupTitles(result.current.navGroups)
    expect(groups.organization).toEqual(['Company Organizations'])
    expect(groups.admin).not.toContain('Organizations')
  })

  it('hides the whole organization group from ordinary users', () => {
    const { result } = sidebarAs(ROLE.USER)
    const groups = groupTitles(result.current.navGroups)
    expect(groups.organization).toBeUndefined()
    expect(groups.personal).not.toContain('Organization')
  })

  it('shows company administrators their organization management entry in the organization group', () => {
    const { result } = sidebarAs(ROLE.USER, {
      id: 3,
      role: 'admin',
      status: 'active',
    })
    const groups = groupTitles(result.current.navGroups)
    expect(groups.organization).toEqual(['My organization'])
    expect(groups.personal).not.toContain('Organization')
  })
})

describe('organization sidebar section settings', () => {
  it('hides the platform entry when the administrator turns it off', () => {
    const { result } = sidebarAs(ROLE.ADMIN, null, {
      organization: { enabled: true, platform: false, company: true },
    })
    const groups = groupTitles(result.current.navGroups)
    expect(groups.organization).toBeUndefined()
  })

  it('hides only the company entry when that switch is off', () => {
    const { result } = sidebarAs(
      ROLE.USER,
      { id: 3, role: 'admin', status: 'active' },
      {
        organization: { enabled: true, platform: true, company: false },
      }
    )
    const groups = groupTitles(result.current.navGroups)
    expect(groups.organization).toBeUndefined()
  })

  it('hides the whole organization group when the section is turned off', () => {
    const { result } = sidebarAs(ROLE.ADMIN, null, {
      organization: { enabled: false, platform: true, company: true },
    })
    expect(groupTitles(result.current.navGroups).organization).toBeUndefined()
  })

  it('keeps the entries visible for saved configurations that predate the section', () => {
    const { result } = sidebarAs(ROLE.ADMIN, null, {
      chat: { enabled: true, playground: true, chat: true },
    })
    expect(groupTitles(result.current.navGroups).organization).toEqual([
      'Company Organizations',
    ])
  })
})

describe('platform operations placement', () => {
  it('lists platform operations in the admin group and hides it when the admin switch is off', () => {
    const { result } = sidebarAs(ROLE.ADMIN, null)
    const groups = groupTitles(result.current.navGroups)
    expect(groups.admin).toContain('Platform operations')
    expect(groups.console ?? []).not.toContain('Platform operations')

    const hidden = sidebarAs(ROLE.ADMIN, null, {
      admin: { enabled: true, platform: false },
    })
    expect(
      groupTitles(hidden.result.current.navGroups).admin ?? []
    ).not.toContain('Platform operations')
  })
})
