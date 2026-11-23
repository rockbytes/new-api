import { zodResolver } from '@hookform/resolvers/zod'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useForm } from 'react-hook-form'
import { describe, expect, test, vi } from 'vitest'

import { Form } from '@/components/ui/form'

import {
  buildSettingJSON,
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  type ChannelFormValues,
} from '../../lib/channel-form'
import { ChannelRateLimitSetting } from '../channel-rate-limit-setting'

vi.mock('@/features/organizations/api', () => ({
  getOrganizations: vi.fn(async () => ({
    page: 1,
    page_size: 100,
    total: 2,
    items: [
      { id: 7, name: 'Acme' },
      { id: 8, name: 'Globex' },
    ],
  })),
}))

function ShareForm(props: { onSave: (value: string) => void }) {
  const form = useForm<ChannelFormValues>({
    resolver: zodResolver(channelFormSchema),
    defaultValues: {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'Shared',
      type: 1,
      key: 'test-key',
      models: 'gpt-4o',
    },
  })
  return (
    <QueryClientProvider client={new QueryClient()}>
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit((values) =>
            props.onSave(buildSettingJSON(values))
          )}
        >
          <ChannelRateLimitSetting />
          <button type='submit'>Save</button>
        </form>
      </Form>
    </QueryClientProvider>
  )
}

// Enables the budget with a valid window and request count, so only the share rows
// decide whether the channel can be saved.
async function enableBudget(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('switch', { name: 'Request budget' }))
  await user.type(
    screen.getByRole('spinbutton', { name: 'Requests per window' }),
    '60'
  )
  await user.type(
    screen.getByRole('spinbutton', { name: 'Window (seconds)' }),
    '60'
  )
}

async function addShare(
  user: ReturnType<typeof userEvent.setup>,
  organizationName: string,
  percent: string
) {
  await user.click(
    screen.getByRole('button', { name: 'Add organization share' })
  )
  const rows = screen.getAllByRole('combobox', { name: 'Organization' })
  const row = rows.at(-1)
  expect(row).toBeDefined()
  await user.click(row as HTMLElement)
  await user.click(
    await screen.findByRole('option', { name: organizationName })
  )
  const percents = screen.getAllByRole('spinbutton', { name: 'Share (%)' })
  const input = percents.at(-1)
  expect(input).toBeDefined()
  if (percent) {
    await user.type(input as HTMLElement, percent)
  }
}

describe('channel organization shares', () => {
  test('a share is saved as a percentage keyed by the organization ID', async () => {
    const user = userEvent.setup()
    const saved: string[] = []
    render(<ShareForm onSave={(value) => saved.push(value)} />)

    await enableBudget(user)
    await addShare(user, 'Acme', '50')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(saved).toHaveLength(1))
    expect(JSON.parse(saved[0]).rate_limit.org_shares).toEqual({ '7': 50 })
  })

  test('a share without a percentage is refused and not saved', async () => {
    const user = userEvent.setup()
    const saved: string[] = []
    render(<ShareForm onSave={(value) => saved.push(value)} />)

    await enableBudget(user)
    await addShare(user, 'Acme', '')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    expect(
      await screen.findByText('Share must be a whole number from 1 to 100')
    ).toBeInTheDocument()
    expect(saved).toHaveLength(0)
  })

  test('two shares for one organization are refused and not saved', async () => {
    const user = userEvent.setup()
    const saved: string[] = []
    render(<ShareForm onSave={(value) => saved.push(value)} />)

    await enableBudget(user)
    await addShare(user, 'Acme', '30')
    await addShare(user, 'Acme', '40')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    expect(
      await screen.findByText('Each organization can have one share')
    ).toBeInTheDocument()
    expect(saved).toHaveLength(0)
  })
})
