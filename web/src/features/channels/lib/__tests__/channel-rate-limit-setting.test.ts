import { assert, describe, expect, test } from 'vitest'

import {
  buildSettingJSON,
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
} from '../channel-form'

describe('channel request budget round trip', () => {
  test('a form save keeps the request budget already stored in the setting', () => {
    const budget = { enabled: true, requests: 60, duration_seconds: 60 }
    const form = channelFormSchema.parse({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'Budgeted',
      type: 1,
      key: 'test-key',
      models: 'gpt-4o',
      rate_limit: budget,
    })

    const setting = JSON.parse(buildSettingJSON(form)) as Record<
      string,
      unknown
    >

    expect(setting.rate_limit).toEqual(budget)
  })

  test('a channel without a budget does not gain one on save', () => {
    const form = channelFormSchema.parse({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'Unbudgeted',
      type: 1,
      key: 'test-key',
      models: 'gpt-4o',
    })

    const setting = JSON.parse(buildSettingJSON(form)) as Record<
      string,
      unknown
    >

    expect(setting).not.toHaveProperty('rate_limit')
  })
})

describe('channel request budget validation', () => {
  const base = {
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'Budgeted',
    type: 1,
    key: 'test-key',
    models: 'gpt-4o',
  }

  test('an enabled budget accepts a whole-number window up to one day', () => {
    const result = channelFormSchema.safeParse({
      ...base,
      rate_limit: { enabled: true, requests: 60, duration_seconds: 86400 },
    })

    expect(result.success).toBe(true)
  })

  test('an enabled budget with a window longer than one day names the window field', () => {
    const result = channelFormSchema.safeParse({
      ...base,
      rate_limit: { enabled: true, requests: 60, duration_seconds: 86401 },
    })

    assert(!result.success)
    expect(result.error.issues.map((issue) => issue.path.join('.'))).toEqual([
      'rate_limit.duration_seconds',
    ])
  })

  test('an enabled budget without a request count names the request field', () => {
    const result = channelFormSchema.safeParse({
      ...base,
      rate_limit: { enabled: true, duration_seconds: 60 },
    })

    assert(!result.success)
    expect(result.error.issues.map((issue) => issue.path.join('.'))).toEqual([
      'rate_limit.requests',
    ])
  })

  test('a disabled budget keeps out-of-range values without blocking the save', () => {
    const result = channelFormSchema.safeParse({
      ...base,
      rate_limit: { enabled: false, requests: 0, duration_seconds: 0 },
    })

    expect(result.success).toBe(true)
  })
})

describe('channel organization share round trip', () => {
  test('a stored share map is edited as rows and saved back as the same map', () => {
    const budget = {
      enabled: true,
      requests: 60,
      duration_seconds: 60,
      org_shares: { '7': 50, '8': 25 },
    }
    const form = channelFormSchema.parse({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'Shared',
      type: 1,
      key: 'test-key',
      models: 'gpt-4o',
      rate_limit: {
        enabled: true,
        requests: 60,
        duration_seconds: 60,
        org_shares: [
          { organization_id: 7, percent: 50 },
          { organization_id: 8, percent: 25 },
        ],
      },
    })

    const setting = JSON.parse(buildSettingJSON(form)) as Record<
      string,
      unknown
    >

    expect(setting.rate_limit).toEqual(budget)
  })

  test('an incomplete share row is refused while the budget is enabled', () => {
    const result = channelFormSchema.safeParse({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'Shared',
      type: 1,
      key: 'test-key',
      models: 'gpt-4o',
      rate_limit: {
        enabled: true,
        requests: 60,
        duration_seconds: 60,
        org_shares: [{ organization_id: 7 }],
      },
    })

    expect(result.success).toBe(false)
  })
})
