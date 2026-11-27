import { z } from 'zod'

import { rateLimitCountSchema } from '@/lib/rate-limits'

export const userRateLimitFormSchema = z.discriminatedUnion('mode', [
  z.object({ mode: z.literal('inherit') }),
  z.object({
    mode: z.literal('custom'),
    total: rateLimitCountSchema,
    success: rateLimitCountSchema,
  }),
])

export type UserRateLimitFormValues = z.infer<typeof userRateLimitFormSchema>
