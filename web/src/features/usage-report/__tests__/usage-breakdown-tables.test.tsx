import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import {
  UsageModelsTable,
  UsageRankingTable,
} from '../components/usage-breakdown-tables'
import type { UsageModelRow, UsageRankRow } from '../types'

function modelRows(count: number): UsageModelRow[] {
  return Array.from({ length: count }, (_, index) => ({
    model_name: `model-${index}`,
    consume_quota: index,
    requests: index,
    prompt_tokens: 0,
    completion_tokens: 0,
  }))
}

function rankRows(count: number): UsageRankRow[] {
  return Array.from({ length: count }, (_, index) => ({
    key: index,
    name: `org-${index}`,
    consume_quota: index,
    requests: index,
  }))
}

describe('UsageModelsTable row limit', () => {
  it('shows only the first 10 rows and no expand toggle at exactly 10 rows', () => {
    render(<UsageModelsTable rows={modelRows(10)} locale='en-US' />)

    expect(screen.getAllByRole('row')).toHaveLength(11)
    expect(
      screen.queryByRole('button', { name: /Show/ })
    ).not.toBeInTheDocument()
  })

  it('reveals the remaining rows after the expand toggle is clicked', async () => {
    render(<UsageModelsTable rows={modelRows(15)} locale='en-US' />)

    expect(screen.getAllByRole('row')).toHaveLength(11)
    const toggle = screen.getByRole('button', { name: 'Show 5 more' })

    await userEvent.click(toggle)

    expect(screen.getAllByRole('row')).toHaveLength(16)
    expect(
      screen.getByRole('button', { name: 'Show less' })
    ).toBeInTheDocument()
  })
})

describe('UsageRankingTable row limit', () => {
  it('reveals the remaining rows after the expand toggle is clicked', async () => {
    render(
      <UsageRankingTable
        rows={rankRows(12)}
        locale='en-US'
        nameFor={(row) => row.name}
      />
    )

    expect(screen.getAllByRole('row')).toHaveLength(11)
    await userEvent.click(screen.getByRole('button', { name: 'Show 2 more' }))

    expect(screen.getAllByRole('row')).toHaveLength(13)
  })
})
