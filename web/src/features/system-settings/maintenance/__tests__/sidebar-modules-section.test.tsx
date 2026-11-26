import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import {
  parseSidebarModulesAdmin,
  serializeSidebarModulesAdmin,
} from '../config'
import { SidebarModulesSection } from '../sidebar-modules-section'

function SettingsFixture() {
  const [actionsContainer, setActionsContainer] =
    useState<HTMLDivElement | null>(null)
  const [config] = useState(() =>
    parseSidebarModulesAdmin('{"console":{"enabled":true,"detail":false}}')
  )
  return (
    <>
      <div ref={setActionsContainer} />
      <SettingsPageProvider actionsContainer={actionsContainer}>
        <SidebarModulesSection
          config={config}
          initialSerialized={serializeSidebarModulesAdmin(config)}
        />
      </SettingsPageProvider>
    </>
  )
}

afterEach(() => vi.restoreAllMocks())

it('saves an individual report switch without changing the other two legacy values', async () => {
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  const client = new QueryClient()
  const user = userEvent.setup()
  render(
    <QueryClientProvider client={client}>
      <SettingsFixture />
    </QueryClientProvider>
  )

  const overview = screen.getByRole('switch', { name: 'Overview' })
  const dashboard = screen.getByRole('switch', { name: 'Dashboard' })
  const report = screen.getByRole('switch', { name: 'Usage report' })
  expect(overview).not.toBeChecked()
  expect(dashboard).not.toBeChecked()
  expect(report).not.toBeChecked()

  await user.click(report)
  expect(report).toBeChecked()
  expect(overview).not.toBeChecked()
  expect(dashboard).not.toBeChecked()
  await user.click(screen.getByRole('button', { name: 'Save sidebar modules' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith('/api/option/', {
      key: 'SidebarModulesAdmin',
      value: expect.any(String),
    })
  )
  const request = put.mock.lastCall?.[1] as { value: string }
  expect(JSON.parse(request.value).console).toMatchObject({
    overview: false,
    detail: false,
    usage_report: true,
  })
})
