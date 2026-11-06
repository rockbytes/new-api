import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { Wallet } from '..'

const noop = vi.fn()

vi.mock('@/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/api')>()),
  getSelf: vi.fn().mockResolvedValue({ success: false }),
}))

vi.mock('../hooks', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../hooks')>()),
  useTopupInfo: () => ({
    topupInfo: undefined,
    presetAmounts: [],
    loading: false,
  }),
  usePayment: () => ({
    amount: 0,
    calculating: false,
    processing: false,
    calculatePaymentAmount: noop,
    processPayment: noop,
  }),
  useAffiliate: () => ({
    affiliateLink: '',
    loading: false,
    transferQuota: noop,
    transferring: false,
  }),
  useRedemption: () => ({ redeeming: false, redeemCode: noop }),
  useCreemPayment: () => ({ processing: false, processCreemPayment: noop }),
  useWaffoPayment: () => ({ processing: false, processWaffoPayment: noop }),
  useWaffoPancakePayment: () => ({
    processing: false,
    processWaffoPancakePayment: noop,
  }),
}))

function renderWallet() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <Wallet />
    </QueryClientProvider>
  )
}

describe('wallet page sections', () => {
  it('hides the add funds and referral cards while their visibility flags are off', async () => {
    renderWallet()

    expect(await screen.findByText('Wallet')).toBeInTheDocument()
    expect(screen.queryByText('Add Funds')).not.toBeInTheDocument()
    expect(screen.queryByText('Referral Program')).not.toBeInTheDocument()
  })
})
