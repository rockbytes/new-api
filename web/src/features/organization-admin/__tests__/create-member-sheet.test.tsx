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
const longGroupName = 'SUP-STDG-PVACP001-华东区域超长分组名称示例'

type RenderProps = {
  withGroupOption?: boolean
  withRoleOption?: boolean
  submit?: (values: unknown) => Promise<void>
}

function renderSheet(props: RenderProps) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <CreateMemberSheet
        open
        onOpenChange={vi.fn()}
        withGroupOption={props.withGroupOption}
        withRoleOption={props.withRoleOption}
        submit={props.submit as never}
      />
    </QueryClientProvider>
  )
}

afterEach(() => {
  vi.clearAllMocks()
})

describe('CreateMemberSheet for platform administrators', () => {
  it('submits the chosen user group and organization role with the account', async () => {
    mockedGroups.mockResolvedValue({
      success: true,
      data: [longGroupName, 'default', 'vip'],
    } as never)
    const submit = vi.fn().mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderSheet({ withGroupOption: true, withRoleOption: true, submit })

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
    mockedGroups.mockResolvedValue({
      success: true,
      data: ['default', 'vip'],
    } as never)
    const submit = vi.fn().mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderSheet({ withGroupOption: true, withRoleOption: true, submit })

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

describe('CreateMemberSheet for organization administrators', () => {
  it('sends only the credentials because the member inherits the admin group', async () => {
    mockedCreate.mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderSheet({})

    expect(screen.queryByRole('combobox', { name: 'User group' })).toBeNull()
    expect(
      screen.queryByRole('combobox', { name: 'Organization role' })
    ).toBeNull()
    await user.type(screen.getByLabelText('Username'), 'frank')
    await user.type(
      screen.getByLabelText('Initial password'),
      'long-enough-pass'
    )
    await user.click(screen.getByRole('button', { name: 'Create' }))

    await waitFor(() => expect(mockedCreate).toHaveBeenCalledTimes(1))
    expect(mockedCreate).toHaveBeenCalledWith({
      username: 'frank',
      password: 'long-enough-pass',
    })
    expect(mockedGroups).not.toHaveBeenCalled()
  })
})

describe('CreateMemberSheet group selector layout', () => {
  it('spans the field width and wraps long group names instead of clipping them', async () => {
    mockedGroups.mockResolvedValue({
      success: true,
      data: [longGroupName],
    } as never)
    const user = userEvent.setup()
    renderSheet({ withGroupOption: true, withRoleOption: true })

    const trigger = await screen.findByRole('combobox', { name: 'User group' })
    expect(trigger).toHaveClass('w-full')

    await user.click(trigger)
    const optionText = await screen.findByText(longGroupName)
    expect(optionText).toHaveClass('break-words', 'whitespace-normal')
  })
})
