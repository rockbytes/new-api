import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'

import { useStatus } from '@/hooks/use-status'
import {
  getServerErrorStatus,
  isServerErrorCancelled,
  requireServerSuccess,
} from '@/lib/server-error-message'

import { getPricing } from '../api'

class CatalogUpdatingError extends Error {}

export function usePricingData({
  enabled = true,
  requireCatalog = false,
} = {}) {
  const { status } = useStatus()

  // Pricing-only consumers keep their existing request contract. The model
  // catalog never caches an older response as a successful catalog snapshot.
  const {
    data: catalog,
    isLoading,
    error,
    refetch,
    isFetching,
    failureReason,
  } = useQuery({
    queryKey: requireCatalog ? ['pricing', 'catalog'] : ['pricing'],
    queryFn: async (context) => {
      const result = requireServerSuccess(
        await getPricing(requireCatalog ? context.signal : undefined)
      )
      if (requireCatalog && result.catalog_capabilities_version !== 1) {
        throw new CatalogUpdatingError()
      }
      return result
    },
    staleTime: 5 * 60 * 1000,
    enabled,
    ...(requireCatalog && {
      retry: (failureCount: number, error: Error) =>
        failureCount < 4 &&
        !isServerErrorCancelled(error) &&
        ![401, 403, 429].includes(getServerErrorStatus(error) ?? 0),
      retryDelay: (attempt: number) => 2000 * 2 ** attempt,
      meta: { errorToast: false },
    }),
  })

  const compatibilityPending =
    !catalog && failureReason instanceof CatalogUpdatingError
  const loadingError =
    requireCatalog && !catalog && !compatibilityPending ? error : null

  // Ensure rates never reach zero to prevent division errors
  const priceRate = useMemo(
    () => Math.max((status?.price as number) ?? 1, 0.001),
    [status?.price]
  )
  const usdExchangeRate = useMemo(
    () => Math.max((status?.usd_exchange_rate as number) ?? priceRate, 0.001),
    [status?.usd_exchange_rate, priceRate]
  )

  const models = useMemo(() => {
    if (!catalog?.data || !catalog?.vendors) return []

    const vendorMap = new Map(catalog.vendors.map((v) => [v.id, v]))

    return catalog.data.map((model) => {
      const vendor = model.vendor_id
        ? vendorMap.get(model.vendor_id)
        : undefined
      return {
        ...model,
        key: model.model_name,
        vendor_name: vendor?.name,
        vendor_icon: vendor?.icon,
        vendor_description: vendor?.description,
        group_ratio: catalog.group_ratio,
      }
    })
  }, [catalog])

  return {
    models,
    vendors: catalog?.vendors ?? [],
    groupRatio: catalog?.group_ratio ?? {},
    usableGroup: catalog?.usable_group ?? {},
    endpointMap:
      catalog?.default_endpoints ?? catalog?.supported_endpoint ?? {},
    autoGroups: catalog?.auto_groups ?? [],
    compatibilityPending,
    loadingError,
    isLoading,
    error,
    refetch,
    isFetching,
    priceRate,
    usdExchangeRate,
  }
}
