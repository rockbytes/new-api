import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { UsageRecordsTable } from '../components/usage-records-table'
import type { UsageSelfRecord } from '../types'

const CONSUME = 2

function record(overrides: Partial<UsageSelfRecord>): UsageSelfRecord {
  return {
    id: 1,
    created_at: 1_700_000_000,
    type: CONSUME,
    username: 'alice',
    token_name: 'key',
    model_name: 'gpt-4o-mini',
    quota: 100,
    prompt_tokens: 10,
    completion_tokens: 5,
    use_time: 1,
    is_stream: false,
    request_id: 'req-1',
    billing_organization_id: 0,
    billing_organization_name: '',
    ...overrides,
  }
}

function renderRecords(props: {
  view: 'self' | 'organization'
  items: UsageSelfRecord[]
  page?: number
  total?: number
  onPageChange?: (page: number) => void
}) {
  return render(
    <UsageRecordsTable
      view={props.view}
      items={props.items}
      total={props.total ?? props.items.length}
      page={props.page ?? 1}
      pageSize={20}
      locale='en-US'
      onPageChange={props.onPageChange ?? vi.fn()}
    />
  )
}

describe('UsageRecordsTable payer column', () => {
  it('shows the payer column for a personal view', () => {
    renderRecords({ view: 'self', items: [record({})] })

    expect(
      screen.getByRole('columnheader', { name: 'Paid by' })
    ).toBeInTheDocument()
  })

  it('omits the payer column for an organization view', () => {
    renderRecords({ view: 'organization', items: [record({})] })

    expect(
      screen.queryByRole('columnheader', { name: 'Paid by' })
    ).not.toBeInTheDocument()
  })
})

describe('UsageRecordsTable amounts', () => {
  it('shows a consume row without a sign and its consumed amount as currency', () => {
    renderRecords({
      view: 'self',
      items: [record({ type: CONSUME, quota: 40 })],
    })

    const row = screen.getByRole('row', { name: /gpt-4o-mini/ })
    expect(row).not.toHaveTextContent('-$')
    expect(row).toHaveTextContent('$0.00008')
  })
})

describe('UsageRecordsTable pagination', () => {
  it('disables Previous on the first page and Next on the last page', () => {
    const { rerender } = renderRecords({
      view: 'self',
      items: [record({})],
      page: 1,
      total: 20,
    })
    expect(screen.getByRole('button', { name: 'Previous' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Next' })).toBeDisabled()

    rerender(
      <UsageRecordsTable
        view='self'
        items={[record({})]}
        total={45}
        page={2}
        pageSize={20}
        locale='en-US'
        onPageChange={vi.fn()}
      />
    )
    expect(screen.getByRole('button', { name: 'Previous' })).toBeEnabled()
    expect(screen.getByRole('button', { name: 'Next' })).toBeEnabled()
  })

  it('requests the next page when Next is clicked', async () => {
    const onPageChange = vi.fn()
    renderRecords({
      view: 'self',
      items: [record({})],
      page: 1,
      total: 45,
      onPageChange,
    })

    await userEvent.click(screen.getByRole('button', { name: 'Next' }))

    expect(onPageChange).toHaveBeenCalledWith(2)
  })
})
