import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, test } from 'vitest'

import {
  applyStoredDataTableDensity,
  DATA_TABLE_DENSITY_STORAGE_KEY,
} from '../../hooks/use-data-table-density'
import { DataTableDensityToggle } from '../density-toggle'

afterEach(() => {
  window.localStorage.clear()
  document.documentElement.removeAttribute('data-table-density')
})

test('choosing compact rows tightens every table and keeps the choice after a reload', () => {
  const { unmount } = render(<DataTableDensityToggle />)
  const toggle = screen.getByRole('button', { name: 'Compact rows' })
  expect(toggle).toHaveAttribute('aria-pressed', 'false')

  fireEvent.click(toggle)

  expect(
    screen.getByRole('button', { name: 'Comfortable rows' })
  ).toHaveAttribute('aria-pressed', 'true')
  expect(document.documentElement).toHaveAttribute(
    'data-table-density',
    'compact'
  )
  expect(window.localStorage.getItem(DATA_TABLE_DENSITY_STORAGE_KEY)).toBe(
    'compact'
  )

  unmount()
  document.documentElement.removeAttribute('data-table-density')
  applyStoredDataTableDensity()
  render(<DataTableDensityToggle />)

  expect(document.documentElement).toHaveAttribute(
    'data-table-density',
    'compact'
  )
  expect(
    screen.getByRole('button', { name: 'Comfortable rows' })
  ).toHaveAttribute('aria-pressed', 'true')
})

test('an unrecognised stored value falls back to comfortable rows', () => {
  window.localStorage.setItem(DATA_TABLE_DENSITY_STORAGE_KEY, 'dense')
  applyStoredDataTableDensity()
  render(<DataTableDensityToggle />)

  expect(document.documentElement).toHaveAttribute(
    'data-table-density',
    'comfortable'
  )
  expect(screen.getByRole('button', { name: 'Compact rows' })).toHaveAttribute(
    'aria-pressed',
    'false'
  )
})
