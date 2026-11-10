import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { getGroups } from '@/features/users/api'

import { createOrganizationMember } from '../api'
import { CreateMemberSheet } from '../components/member-sheets'

vi.mock('../api', () => ({
  createOrganizationMember: vi.fn(),
  resetOrganizationMemberPassword: vi.fn(),
}))

vi.mock('@/features/users/api', () => ({
  getGroups: vi.fn(),
}))

const mockedCreate = vi.mocked(createOrganizationMember)
const mockedGroups = vi.mocked(getGroups)

function renderSheet(props: {
  withAdminOptions?: boolean
  submit?: (values: unknown) => Promise<void>
}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <CreateMemberSheet
        open
        onOpenChange={vi.fn()}
        withAdminOptions={props.withAdminOptions}
        submit={props.submit as never}
      />
    </QueryClientProvider>
  )
}

function mockGroups() {
  mockedGroups.mockResolvedValue({
    success: true,
    data: ['default', 'vip'],
  } as never)
}

afterEach(() => {
  vi.clearAllMocks()
})

describe('CreateMemberSheet for platform administrators', () => {
  it('submits the chosen user group and organization role with the account', async () => {
    mockGroups()
    const submit = vi.fn().mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderSheet({ withAdminOptions: true, submit })

    await user.type(screen.getByLabelText('Username'), 'dana')
    await user.type(
      screen.getByLabelText('Initial password'),
      'long-enough-pass'
    )
    await user.click(
      await screen.findByRole('combobox', { name: 'User group' })
    )
    await user.click(await screen.findByRole('option', { name: 'vip' }))
    await user.click(
      screen.getByRole('combobox', { name: 'Organization role' })
    )
    await user.click(await screen.findByRole('option', { name: 'Admin' }))
    await user.click(screen.getByRole('button', { name: 'Create' }))

    await waitFor(() => expect(submit).toHaveBeenCalledTimes(1))
    expect(submit).toHaveBeenCalledWith({
      username: 'dana',
      password: 'long-enough-pass',
      group: 'vip',
      role: 'admin',
    })
  })

  it('does not create the account until a user group is chosen', async () => {
    mockGroups()
    const submit = vi.fn().mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderSheet({ withAdminOptions: true, submit })

    await user.type(screen.getByLabelText('Username'), 'dana')
    await user.type(
      screen.getByLabelText('Initial password'),
      'long-enough-pass'
    )
    await user.click(screen.getByRole('button', { name: 'Create' }))

    // The placeholder shows the same text, so look for the validation message only.
    expect(
      await screen.findByText('Select a group', {
        selector: '[data-slot="form-message"]',
      })
    ).toBeVisible()
    expect(submit).not.toHaveBeenCalled()
  })
})

describe('CreateMemberSheet from the organization admin page', () => {
  it('hides the group and role fields and sends only the credentials', async () => {
    mockedCreate.mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderSheet({})

    expect(screen.queryByRole('combobox', { name: 'User group' })).toBeNull()
    expect(
      screen.queryByRole('combobox', { name: 'Organization role' })
    ).toBeNull()
    await user.type(screen.getByLabelText('Username'), 'erin')
    await user.type(
      screen.getByLabelText('Initial password'),
      'long-enough-pass'
    )
    await user.click(screen.getByRole('button', { name: 'Create' }))

    await waitFor(() => expect(mockedCreate).toHaveBeenCalledTimes(1))
    expect(mockedCreate).toHaveBeenCalledWith({
      username: 'erin',
      password: 'long-enough-pass',
    })
  })
})
