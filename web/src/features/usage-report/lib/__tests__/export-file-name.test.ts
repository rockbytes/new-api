import { describe, expect, it } from 'vitest'

import { usageExportFileName } from '../export'

describe('usageExportFileName', () => {
  it('takes the file name the server put in Content-Disposition', () => {
    expect(
      usageExportFileName('attachment; filename="usage-records-20261106.csv"')
    ).toBe('usage-records-20261106.csv')
  })

  it('falls back to a neutral name when the header is missing', () => {
    expect(usageExportFileName(undefined)).toBe('usage-export')
  })

  it('falls back when the header carries no quoted file name', () => {
    expect(usageExportFileName('attachment')).toBe('usage-export')
  })
})
