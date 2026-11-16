import { useState } from 'react'

import { USAGE_DEFAULT_VISIBLE_ROWS } from '../constants'

// Shared by the Trend, Models, and Ranking lists: each shows the first rows and expands in place.
export function useExpandableRows<T>(rows: T[]) {
  const [expanded, setExpanded] = useState(false)
  const hiddenCount = Math.max(0, rows.length - USAGE_DEFAULT_VISIBLE_ROWS)
  const visible = expanded ? rows : rows.slice(0, USAGE_DEFAULT_VISIBLE_ROWS)
  return {
    visible,
    hasMore: hiddenCount > 0,
    hiddenCount,
    expanded,
    toggle: () => setExpanded((value) => !value),
  }
}
