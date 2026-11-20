import { zodResolver } from '@hookform/resolvers/zod'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useForm } from 'react-hook-form'
import { describe, expect, test } from 'vitest'

import { Form } from '@/components/ui/form'

import {
  buildSettingJSON,
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  type ChannelFormValues,
} from '../../lib/channel-form'
import { ChannelRateLimitSetting } from '../channel-rate-limit-setting'

function BudgetForm(props: { onSave: (value: string) => void }) {
  const form = useForm<ChannelFormValues>({
    resolver: zodResolver(channelFormSchema),
    defaultValues: {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'Budgeted',
      type: 1,
      key: 'test-key',
      models: 'gpt-4o',
    },
  })
  return (
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
  )
}

describe('channel request budget setting', () => {
  test('the budget fields stay hidden until the budget is switched on', async () => {
    const user = userEvent.setup()
    render(<BudgetForm onSave={() => undefined} />)

    expect(
      screen.queryByRole('spinbutton', { name: 'Requests per window' })
    ).not.toBeInTheDocument()

    await user.click(screen.getByRole('switch', { name: 'Request budget' }))

    expect(
      screen.getByRole('spinbutton', { name: 'Requests per window' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('spinbutton', { name: 'Window (seconds)' })
    ).toBeInTheDocument()
  })

  test('a budget entered in the form is saved with the channel setting', async () => {
    const user = userEvent.setup()
    const saved: string[] = []
    render(<BudgetForm onSave={(value) => saved.push(value)} />)

    await user.click(screen.getByRole('switch', { name: 'Request budget' }))
    await user.type(
      screen.getByRole('spinbutton', { name: 'Requests per window' }),
      '60'
    )
    await user.type(
      screen.getByRole('spinbutton', { name: 'Window (seconds)' }),
      '60'
    )
    await user.click(screen.getByRole('button', { name: 'Save' }))

    expect(JSON.parse(saved[0]).rate_limit).toEqual({
      enabled: true,
      requests: 60,
      duration_seconds: 60,
    })
  })

  test('an enabled budget with a window longer than one day is refused and not saved', async () => {
    const user = userEvent.setup()
    const saved: string[] = []
    render(<BudgetForm onSave={(value) => saved.push(value)} />)

    await user.click(screen.getByRole('switch', { name: 'Request budget' }))
    await user.type(
      screen.getByRole('spinbutton', { name: 'Requests per window' }),
      '60'
    )
    await user.type(
      screen.getByRole('spinbutton', { name: 'Window (seconds)' }),
      '86401'
    )
    await user.click(screen.getByRole('button', { name: 'Save' }))

    expect(
      await screen.findByText(
        'Window must be a whole number of seconds from 1 to 86400'
      )
    ).toBeInTheDocument()
    expect(saved).toHaveLength(0)
  })
})
