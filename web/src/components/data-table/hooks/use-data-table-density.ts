import * as React from 'react'

export const DATA_TABLE_DENSITIES = {
  COMFORTABLE: 'comfortable',
  COMPACT: 'compact',
} as const

export type DataTableDensity =
  (typeof DATA_TABLE_DENSITIES)[keyof typeof DATA_TABLE_DENSITIES]

export const DATA_TABLE_DENSITY_STORAGE_KEY = 'newapi:table-density:v1'

const DENSITY_ATTRIBUTE = 'data-table-density'

function isDensity(value: unknown): value is DataTableDensity {
  return (
    value === DATA_TABLE_DENSITIES.COMFORTABLE ||
    value === DATA_TABLE_DENSITIES.COMPACT
  )
}

function readStoredDensity(): DataTableDensity {
  try {
    const raw = window.localStorage.getItem(DATA_TABLE_DENSITY_STORAGE_KEY)
    return isDensity(raw) ? raw : DATA_TABLE_DENSITIES.COMFORTABLE
  } catch {
    return DATA_TABLE_DENSITIES.COMFORTABLE
  }
}

function applyDensity(density: DataTableDensity) {
  document.documentElement.setAttribute(DENSITY_ATTRIBUTE, density)
}

/**
 * Applies the stored table density before the first render so tables that
 * mount later already use the saved row height.
 */
export function applyStoredDataTableDensity() {
  applyDensity(readStoredDensity())
}

/**
 * Global table density preference. The choice is stored in localStorage and
 * reflected on <html> as `data-table-density`, which the table cell styles
 * read through CSS variables. Storage failures fall back to in-memory state.
 */
export function useDataTableDensity(): [
  DataTableDensity,
  (density: DataTableDensity) => void,
] {
  const [density, setDensityState] =
    React.useState<DataTableDensity>(readStoredDensity)

  const setDensity = React.useCallback((next: DataTableDensity) => {
    try {
      window.localStorage.setItem(DATA_TABLE_DENSITY_STORAGE_KEY, next)
    } catch {
      // Without storage the preference only lasts for this page session.
    }
    applyDensity(next)
    setDensityState(next)
  }, [])

  return [density, setDensity]
}
