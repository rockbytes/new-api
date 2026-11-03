import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, test } from 'vitest'

import { THEME_STORAGE_KEYS } from '@/lib/theme-storage'

import {
  ThemeCustomizationProvider,
  useThemeCustomization,
} from '../theme-customization-provider'

function LayoutProbe() {
  const { customization, setContentLayout, resetCustomization } =
    useThemeCustomization()
  return (
    <div>
      <p>layout: {customization.contentLayout}</p>
      <button type='button' onClick={() => setContentLayout('centered')}>
        Center
      </button>
      <button type='button' onClick={() => resetCustomization()}>
        Reset
      </button>
    </div>
  )
}

function renderLayoutProbe() {
  return render(
    <ThemeCustomizationProvider>
      <LayoutProbe />
    </ThemeCustomizationProvider>
  )
}

beforeEach(() => {
  window.localStorage.clear()
  document.body.removeAttribute('data-theme-content-layout')
})

afterEach(() => {
  window.localStorage.clear()
  document.body.removeAttribute('data-theme-content-layout')
})

test('starts with the full content layout when nothing is stored', () => {
  renderLayoutProbe()
  expect(screen.getByText('layout: full')).toBeInTheDocument()
  expect(document.body).toHaveAttribute('data-theme-content-layout', 'full')
})

test('keeps the centered content layout after remounting', () => {
  const first = renderLayoutProbe()
  fireEvent.click(screen.getByRole('button', { name: 'Center' }))
  expect(document.body).toHaveAttribute('data-theme-content-layout', 'centered')
  first.unmount()

  renderLayoutProbe()
  expect(screen.getByText('layout: centered')).toBeInTheDocument()
})

test('reset returns the content layout to full', () => {
  renderLayoutProbe()
  fireEvent.click(screen.getByRole('button', { name: 'Center' }))
  fireEvent.click(screen.getByRole('button', { name: 'Reset' }))
  expect(screen.getByText('layout: full')).toBeInTheDocument()
  expect(document.body).toHaveAttribute('data-theme-content-layout', 'full')
})

test('ignores an unrecognised stored layout and uses full', () => {
  window.localStorage.setItem(THEME_STORAGE_KEYS.contentLayout, 'sideways')
  renderLayoutProbe()
  expect(screen.getByText('layout: full')).toBeInTheDocument()
})
