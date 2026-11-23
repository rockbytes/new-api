import * as z from 'zod'

// Matches the largest model request limit the server accepts.
export const MAX_USER_REQUEST_LIMIT = 100000000

// 0 means the override does not set that limit, so the global default applies.
export const createUserRequestLimitSchema = (t: (key: string) => string) => {
  const invalidNumber = t('Enter a whole number of requests.')
  const outOfRange = t('Enter a number between 0 and 100000000.')
  const countSchema = z
    .number({ error: invalidNumber })
    .int({ error: invalidNumber })
    .min(0, { error: outOfRange })
    .max(MAX_USER_REQUEST_LIMIT, { error: outOfRange })
  return z
    .object({
      total_count: countSchema,
      success_count: countSchema,
    })
    .refine((values) => values.total_count > 0 || values.success_count > 0, {
      error: t('Enter at least one limit. Use Remove limits to clear them.'),
      path: ['total_count'],
    })
}

export type UserRequestLimitFormValues = z.infer<
  ReturnType<typeof createUserRequestLimitSchema>
>
