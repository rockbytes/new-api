import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { getGroups } from '@/features/users/api'

import { createOrganizationMember, getUsableGroupNames } from '../api'
import { CreateMemberSheet } from '../components/member-sheets'

vi.mock('../api', () => ({
  createOrganizationMember: vi.fn(),
  getUsableGroupNames: vi.fn(),
  resetOrganizationMemberPassword: vi.fn(),
}))

vi.mock('@/features/users/api', () => ({
  getGroups: vi.fn(),
}))

const mockedCreate = vi.mocked(createOrganizationMember)
const mockedGroups = vi.mocked(getGroups)
const mockedUsableGroups = vi.mocked(getUsableGroupNames)

type RenderProps = {
  withGroupOption?: boolean
  groupScope?: 'all' | 'usable'
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
        groupScope={props.groupScope}
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
      data: ['default', 'vip'],
    } as never)
    const submit = vi.fn().mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderSheet({
      withGroupOption: true,
      groupScope: 'all',
      withRoleOption: true,
      submit,
    })

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
    renderSheet({
      withGroupOption: true,
      groupScope: 'all',
      withRoleOption: true,
      submit,
    })

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
  it('offers only the usable groups and sends the chosen group without a role', async () => {
    mockedUsableGroups.mockResolvedValue(['vip'])
    mockedGroups.mockResolvedValue({
      success: true,
      data: ['default', 'vip', 'hidden'],
    } as never)
    mockedCreate.mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderSheet({ withGroupOption: true })

    expect(
      screen.queryByRole('combobox', { name: 'Organization role' })
    ).toBeNull()
    await user.type(screen.getByLabelText('Username'), 'erin')
    await user.type(
      screen.getByLabelText('Initial password'),
      'long-enough-pass'
    )
    await user.click(
      await screen.findByRole('combobox', { name: 'User group' })
    )
    expect(await screen.findByRole('option', { name: 'vip' })).toBeVisible()
    expect(screen.queryByRole('option', { name: 'hidden' })).toBeNull()
    await user.click(screen.getByRole('option', { name: 'vip' }))
    await user.click(screen.getByRole('button', { name: 'Create' }))

    await waitFor(() => expect(mockedCreate).toHaveBeenCalledTimes(1))
    expect(mockedCreate).toHaveBeenCalledWith({
      username: 'erin',
      password: 'long-enough-pass',
      group: 'vip',
    })
  })
})

describe('CreateMemberSheet without options', () => {
  it('sends only the credentials when no group option is shown', async () => {
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
  })
})

describe('CreateMemberSheet group selector layout', () => {
  it('spans the field width and wraps long group names instead of clipping them', async () => {
    const longGroup = 'SUP-STDG-PVACP001-华东区域超长分组名称示例'
    mockedUsableGroups.mockResolvedValue([longGroup])
    const user = userEvent.setup()
    renderSheet({ withGroupOption: true })

    const trigger = await screen.findByRole('combobox', { name: 'User group' })
    expect(trigger).toHaveClass('w-full')

    await user.click(trigger)
    const optionText = await screen.findByText(longGroup)
    expect(optionText).toHaveClass('break-words', 'whitespace-normal')
  })
})
