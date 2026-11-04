import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/**
 * Generates page numbers for pagination with ellipsis
 * @param currentPage - Current page number (1-based)
 * @param totalPages - Total number of pages
 * @returns Array of page numbers and ellipsis strings
 *
 * Examples:
 * - Small dataset (≤4 pages): [1, 2, 3, 4]
 * - Near beginning: [1, 2, '...', 10]
 * - In middle: [1, '...', 5, '...', 10]
 * - Near end: [1, '...', 9, 10]
 */
export function getPageNumbers(currentPage: number, totalPages: number) {
  const maxVisiblePages = 4
  const rangeWithDots = []

  if (totalPages <= maxVisiblePages) {
    for (let i = 1; i <= totalPages; i++) {
      rangeWithDots.push(i)
    }
  } else {
    rangeWithDots.push(1)

    if (currentPage <= 2) {
      rangeWithDots.push(2)
      rangeWithDots.push('...', totalPages)
    } else if (currentPage >= totalPages - 1) {
      rangeWithDots.push('...')
      rangeWithDots.push(totalPages - 1, totalPages)
    } else {
      rangeWithDots.push('...')
      rangeWithDots.push(currentPage)
      rangeWithDots.push('...', totalPages)
    }
  }

  return rangeWithDots
}

/**
 * Truncate text to a maximum length with ellipsis
 */
export function truncateText(text: string, maxLength: number): string {
  if (!text || text.length <= maxLength) return text
  return `${text.slice(0, maxLength)}...`
}
