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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { QuotaAdjustDialog } from '../quota-adjust-dialog'

describe('QuotaAdjustDialog', () => {
  it('passes the chosen mode and a positive amount when subtracting', async () => {
    const onConfirm = vi.fn().mockResolvedValue(true)
    const onOpenChange = vi.fn()
    const user = userEvent.setup()
    render(
      <QuotaAdjustDialog
        open
        onOpenChange={onOpenChange}
        currentQuota={5_000_000}
        onConfirm={onConfirm}
      />
    )

    await user.click(screen.getByRole('button', { name: 'Subtract' }))
    await user.type(screen.getByRole('spinbutton'), '1')
    await user.click(screen.getByRole('button', { name: 'Confirm' }))

    await waitFor(() => expect(onConfirm).toHaveBeenCalledTimes(1))
    const [mode, value] = onConfirm.mock.calls[0]
    expect(mode).toBe('subtract')
    expect(value).toBeGreaterThan(0)
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
  })

  it('stays open when the change is not applied', async () => {
    const onConfirm = vi.fn().mockResolvedValue(false)
    const onOpenChange = vi.fn()
    const user = userEvent.setup()
    render(
      <QuotaAdjustDialog
        open
        onOpenChange={onOpenChange}
        currentQuota={0}
        onConfirm={onConfirm}
      />
    )

    await user.type(screen.getByRole('spinbutton'), '1')
    await user.click(screen.getByRole('button', { name: 'Confirm' }))

    await waitFor(() => expect(onConfirm).toHaveBeenCalledTimes(1))
    expect(onOpenChange).not.toHaveBeenCalledWith(false)
  })

  it('refuses a subtraction that would take the balance below the minimum', async () => {
    const onConfirm = vi.fn().mockResolvedValue(true)
    const user = userEvent.setup()
    render(
      <QuotaAdjustDialog
        open
        onOpenChange={vi.fn()}
        currentQuota={5_000_000}
        minimumQuota={0}
        onConfirm={onConfirm}
      />
    )

    await user.click(screen.getByRole('button', { name: 'Subtract' }))
    await user.type(screen.getByRole('spinbutton'), '100')

    expect(
      screen.getByText('Amount exceeds the available quota.')
    ).toBeVisible()
    expect(screen.getByRole('button', { name: 'Confirm' })).toBeDisabled()
    expect(screen.queryByText(/-\$/)).not.toBeInTheDocument()
    expect(onConfirm).not.toHaveBeenCalled()
  })
})
