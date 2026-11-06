import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { Profile } from '..'

const noop = vi.fn()

vi.mock('@/hooks/use-status', () => ({
  useStatus: () => ({ status: {} }),
}))

vi.mock('../hooks', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../hooks')>()),
  useProfile: () => ({
    profile: null,
    loading: false,
    refreshProfile: noop,
  }),
}))

describe('profile page sections', () => {
  it('hides the settings card and keeps the language preferences card', async () => {
    render(<Profile />)

    expect(await screen.findByText('Language Preferences')).toBeInTheDocument()
    expect(screen.queryByText('Settings & Preferences')).not.toBeInTheDocument()
  })
})
