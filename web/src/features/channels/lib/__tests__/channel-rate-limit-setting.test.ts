import { describe, expect, test } from 'vitest'

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

    const setting = JSON.parse(buildSettingJSON(form)) as Record<string, unknown>

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

    const setting = JSON.parse(buildSettingJSON(form)) as Record<string, unknown>

    expect(setting).not.toHaveProperty('rate_limit')
  })
})
