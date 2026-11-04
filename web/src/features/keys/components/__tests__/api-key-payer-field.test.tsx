import { render, screen } from '@testing-library/react'
import { useForm } from 'react-hook-form'
import { describe, expect, it } from 'vitest'

import { Form } from '@/components/ui/form'

import type { ApiKeyFormValues } from '../../lib/api-key-form'
import type { PayerOption } from '../../lib/billing-organization'
import { ApiKeyPayerField } from '../api-key-payer-field'

const options: PayerOption[] = [
  { value: 'personal', label: 'Personal' },
  { value: '7', label: 'Shanghai Co' },
]

function PayerFieldHarness(props: { payer: number | null }) {
  const form = useForm<ApiKeyFormValues>({
    defaultValues: { billing_organization_id: props.payer } as ApiKeyFormValues,
  })
  return (
    <Form {...form}>
      <ApiKeyPayerField control={form.control} options={options} />
    </Form>
  )
}

describe('ApiKeyPayerField', () => {
  it('shows the label of the personal payer rather than its internal value', () => {
    render(<PayerFieldHarness payer={null} />)

    const trigger = screen.getByRole('combobox')
    expect(trigger).toHaveTextContent('Personal')
    expect(trigger).not.toHaveTextContent('personal')
  })

  it('shows the organization name when the key is paid by that organization', () => {
    render(<PayerFieldHarness payer={7} />)

    expect(screen.getByRole('combobox')).toHaveTextContent('Shanghai Co')
  })
})
