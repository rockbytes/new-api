import { act, renderHook } from '@testing-library/react'
import { expect, test } from 'vitest'

import { useExpandableRows } from '../use-expandable-rows'

function days(count: number): number[] {
  return Array.from({ length: count }, (_, index) => index)
}

test('shows at most the default visible rows while collapsed', () => {
  const { result } = renderHook(() => useExpandableRows(days(15)))

  expect(result.current.visible).toHaveLength(10)
  expect(result.current.hasMore).toBe(true)
  expect(result.current.hiddenCount).toBe(5)
  expect(result.current.expanded).toBe(false)
})

test('reveals every row once toggled, then collapses back on a second toggle', () => {
  const { result } = renderHook(() => useExpandableRows(days(15)))

  act(() => result.current.toggle())
  expect(result.current.visible).toHaveLength(15)
  expect(result.current.expanded).toBe(true)

  act(() => result.current.toggle())
  expect(result.current.visible).toHaveLength(10)
  expect(result.current.expanded).toBe(false)
})

test('reports no more rows when the list is at or under the default', () => {
  const { result } = renderHook(() => useExpandableRows(days(10)))

  expect(result.current.visible).toHaveLength(10)
  expect(result.current.hasMore).toBe(false)
  expect(result.current.hiddenCount).toBe(0)
})
