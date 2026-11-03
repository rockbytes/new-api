import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'

import { AllocationSheet } from '../components/allocation-sheet'

function renderSheet(onSubmit: (allocated: number) => void) {
  return render(
    <AllocationSheet
      open
      onOpenChange={() => undefined}
      username='bob'
      allocated={10}
      used={0}
      isPending={false}
      onSubmit={onSubmit}
    />
  )
}

it('rejects a negative allocation without submitting', async () => {
  const onSubmit = vi.fn()
  renderSheet(onSubmit)

  const input = screen.getByLabelText('Allocated quota')
  await userEvent.clear(input)
  await userEvent.type(input, '-5')
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))

  expect(
    await screen.findByText('Quota cannot be negative.')
  ).toBeInTheDocument()
  expect(onSubmit).not.toHaveBeenCalled()
})

it('rejects a fractional allocation without submitting', async () => {
  const onSubmit = vi.fn()
  renderSheet(onSubmit)

  const input = screen.getByLabelText('Allocated quota')
  await userEvent.clear(input)
  await userEvent.type(input, '2.5')
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))

  expect(
    await screen.findByText('Enter a whole number of quota.')
  ).toBeInTheDocument()
  expect(onSubmit).not.toHaveBeenCalled()
})

it('submits a whole-number allocation as entered', async () => {
  const onSubmit = vi.fn()
  renderSheet(onSubmit)

  const input = screen.getByLabelText('Allocated quota')
  await userEvent.clear(input)
  await userEvent.type(input, '25')
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))

  await vi.waitFor(() => expect(onSubmit).toHaveBeenCalledWith(25))
})
