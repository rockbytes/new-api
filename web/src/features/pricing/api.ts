import { api } from '@/lib/api'

import type { PricingData } from './types'

// ----------------------------------------------------------------------------
// Pricing APIs
// ----------------------------------------------------------------------------

// Get model pricing data
export async function getPricing(signal?: AbortSignal): Promise<PricingData> {
  const res = await api.get(
    '/api/pricing',
    signal ? { signal, disableDuplicate: true } : undefined
  )
  return res.data
}
