import * as z from 'zod'

// Matches the largest model request limit the server accepts.
export const MAX_ORGANIZATION_REQUEST_LIMIT = 100000000

export const createRequestLimitSchema = (t: (key: string) => string) => {
  const invalidNumber = t('Enter a whole number of requests.')
  const outOfRange = t('Enter a number between 1 and 100000000.')
  return z.object({
    total_count: z
      .number({ error: invalidNumber })
      .int({ error: invalidNumber })
      .min(1, { error: outOfRange })
      .max(MAX_ORGANIZATION_REQUEST_LIMIT, { error: outOfRange }),
  })
}

export type RequestLimitFormValues = z.infer<
  ReturnType<typeof createRequestLimitSchema>
>
