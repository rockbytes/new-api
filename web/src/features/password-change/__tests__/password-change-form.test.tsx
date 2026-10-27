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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { PasswordChange } from '..'

const OLD_PASSWORD = 'old-password-1'
const NEW_PASSWORD = 'new-password-2'

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <PasswordChange />
    </QueryClientProvider>
  )
}

async function fillForm(confirmPassword: string, newPassword = NEW_PASSWORD) {
  const user = userEvent.setup()
  await user.type(screen.getByLabelText('Current password'), OLD_PASSWORD)
  await user.type(screen.getByLabelText('New password'), newPassword)
  await user.type(
    screen.getByLabelText('Confirm new password'),
    confirmPassword
  )
  await user.click(screen.getByRole('button', { name: 'Change password' }))
}

afterEach(() => {
  vi.restoreAllMocks()
})

it('shows a mismatch error and does not submit when the confirmation differs', async () => {
  const put = vi.spyOn(api, 'put')

  renderPage()
  await fillForm('different-3')

  expect(
    await screen.findByText('The passwords do not match.')
  ).toBeInTheDocument()
  expect(put).not.toHaveBeenCalled()
})

it('rejects a new password that equals the current password without submitting', async () => {
  const put = vi.spyOn(api, 'put')

  renderPage()
  await fillForm(OLD_PASSWORD, OLD_PASSWORD)

  expect(
    await screen.findByText(
      'The new password must be different from the current password.'
    )
  ).toBeInTheDocument()
  expect(put).not.toHaveBeenCalled()
})
