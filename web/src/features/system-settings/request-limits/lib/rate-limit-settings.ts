import { z } from 'zod'

import { RATE_LIMIT_WINDOW_MAX, rateLimitCountSchema } from '@/lib/rate-limits'

const groupLimitsSchema = z.record(
  z.string().min(1),
  z.tuple([rateLimitCountSchema, rateLimitCountSchema])
)

const WINDOW_ERROR = 'Enter a whole number from 0 to 1,440.'

export const rateLimitSettingsSchema = z
  .object({
    ModelRequestRateLimitEnabled: z.boolean(),
    ModelRequestRateLimitDurationMinutes: z
      .number(WINDOW_ERROR)
      .int(WINDOW_ERROR)
      .min(0, WINDOW_ERROR)
      .max(RATE_LIMIT_WINDOW_MAX, WINDOW_ERROR),
    ModelRequestRateLimitCount: rateLimitCountSchema,
    ModelRequestRateLimitSuccessCount: rateLimitCountSchema,
    ModelRequestRateLimitGroup: z.string().refine((value) => {
      if (value.trim() === '') return true
      try {
        return groupLimitsSchema.safeParse(JSON.parse(value)).success
      } catch {
        return false
      }
    }, 'Invalid JSON format or values out of allowed range'),
  })
  .refine(
    (settings) =>
      !settings.ModelRequestRateLimitEnabled ||
      settings.ModelRequestRateLimitDurationMinutes > 0,
    {
      path: ['ModelRequestRateLimitDurationMinutes'],
      message: 'Use at least 1 minute when rate limiting is enabled.',
    }
  )

export type RateLimitSettings = z.infer<typeof rateLimitSettingsSchema>
