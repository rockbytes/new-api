import { z } from 'zod'

export const RATE_LIMIT_COUNT_MAX = 2_147_483_647
export const RATE_LIMIT_WINDOW_MAX = 1440

const COUNT_ERROR = 'Enter a whole number from 0 to 2,147,483,647.'

export const rateLimitCountSchema = z
  .number(COUNT_ERROR)
  .int(COUNT_ERROR)
  .min(0, COUNT_ERROR)
  .max(RATE_LIMIT_COUNT_MAX, COUNT_ERROR)

export type RateLimitCounts = { total: number; success: number }
