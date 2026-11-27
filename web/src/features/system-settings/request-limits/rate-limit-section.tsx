import { zodResolver } from '@hookform/resolvers/zod'
import { Code2, Palette } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { JsonCodeEditor } from '@/components/json-code-editor'
import { RateLimitCountInput } from '@/components/rate-limit-count-input'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { RATE_LIMIT_WINDOW_MAX } from '@/lib/rate-limits'

import {
  SettingsControlGroup,
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import {
  rateLimitSettingsSchema,
  type RateLimitSettings,
} from './lib/rate-limit-settings'
import { RateLimitVisualEditor } from './rate-limit-visual-editor'

const ENABLED_OPTION = 'ModelRequestRateLimitEnabled'

export function RateLimitSection(props: { defaultValues: RateLimitSettings }) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [useVisualEditor, setUseVisualEditor] = useState(true)
  const form = useForm<RateLimitSettings>({
    resolver: zodResolver(rateLimitSettingsSchema),
    mode: 'onChange',
    defaultValues: props.defaultValues,
  })
  const enabled = useWatch({ control: form.control, name: ENABLED_OPTION })

  useEffect(() => {
    form.reset(props.defaultValues)
  }, [props.defaultValues, form])

  const onSubmit = async (values: RateLimitSettings) => {
    const enabledChanged =
      values[ENABLED_OPTION] !== props.defaultValues[ENABLED_OPTION]
    const updates = Object.entries(values).filter(
      ([key, value]) =>
        key !== ENABLED_OPTION &&
        value !== props.defaultValues[key as keyof RateLimitSettings]
    )

    // Options are written separately. A zero window is only valid while disabled.
    if (enabledChanged) {
      const enableUpdate: [string, boolean] = [
        ENABLED_OPTION,
        values[ENABLED_OPTION],
      ]
      if (values[ENABLED_OPTION]) updates.push(enableUpdate)
      else updates.unshift(enableUpdate)
    }

    try {
      for (const [key, value] of updates) {
        await updateOption.mutateAsync({
          key,
          // Serialize integers as JSON numbers (the server rejects forms like 1e3).
          value:
            key === 'ModelRequestRateLimitGroup'
              ? JSON.stringify(
                  JSON.parse(String(value).trim() || '{}'),
                  null,
                  2
                )
              : value,
        })
      }
    } catch {
      // useUpdateOption reports the failure. Do not write any remaining options.
    }
  }

  return (
    <SettingsSection title={t('Rate Limiting')}>
      <Form {...form}>
        <SettingsForm noValidate onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={form.formState.isSubmitting}
            saveLabel='Save rate limits'
          />
          <SettingsControlGroup>
            <h3 className='font-medium'>{t('Model request rate limiting')}</h3>
            <FormField
              control={form.control}
              name={ENABLED_OPTION}
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Enable rate limiting')}</FormLabel>
                    <FormDescription>
                      {t(
                        'This controls model request rate limiting. Web/API route throttling is configured by environment variables and may still return 429.'
                      )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={(checked) => {
                        field.onChange(checked)
                        void form.trigger(
                          'ModelRequestRateLimitDurationMinutes'
                        )
                      }}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
            {!enabled && (
              <Alert>
                <AlertDescription>
                  {t(
                    'Saved limits are not enforced while model request rate limiting is off.'
                  )}
                </AlertDescription>
              </Alert>
            )}
            <FormField
              control={form.control}
              name='ModelRequestRateLimitDurationMinutes'
              render={({ field }) => (
                <FormItem className='sm:max-w-md'>
                  <FormLabel>{t('Limit period')}</FormLabel>
                  <div className='flex items-center gap-2'>
                    <FormControl>
                      <Input
                        {...field}
                        type='number'
                        min={enabled ? 1 : 0}
                        max={RATE_LIMIT_WINDOW_MAX}
                        step={1}
                        value={Number.isFinite(field.value) ? field.value : ''}
                        onChange={(event) =>
                          field.onChange(event.target.valueAsNumber)
                        }
                      />
                    </FormControl>
                    <span className='text-muted-foreground text-sm'>
                      {t('minutes')}
                    </span>
                  </div>
                  <FormDescription>
                    {t(
                      'User and group limits use this same time window in minutes.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </SettingsControlGroup>

          <SettingsControlGroup>
            <h3 className='font-medium'>{t('Default limits per user')}</h3>
            <p className='text-muted-foreground text-sm'>
              {t(
                'Each user is counted separately. These defaults apply only to users without custom limits.'
              )}
            </p>
            <div className='grid gap-4 sm:grid-cols-2'>
              <FormField
                control={form.control}
                name='ModelRequestRateLimitCount'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t('Max Requests (including failures)')}
                    </FormLabel>
                    <FormControl>
                      <RateLimitCountInput {...field} />
                    </FormControl>
                    <FormDescription>{t('0 means unlimited')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='ModelRequestRateLimitSuccessCount'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Max Successful Requests')}</FormLabel>
                    <FormControl>
                      <RateLimitCountInput {...field} />
                    </FormControl>
                    <FormDescription>{t('0 means unlimited')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
          </SettingsControlGroup>

          <SettingsControlGroup>
            <FormField
              control={form.control}
              name='ModelRequestRateLimitGroup'
              render={({ field }) => (
                <FormItem>
                  <div className='flex flex-wrap items-center justify-between gap-2'>
                    <h3 className='font-medium'>
                      {useVisualEditor ? (
                        t('Additional limits by request group')
                      ) : (
                        <FormLabel>
                          {t('Additional limits by request group')}
                        </FormLabel>
                      )}
                    </h3>
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      onClick={() => setUseVisualEditor((value) => !value)}
                    >
                      {useVisualEditor ? (
                        <Code2 aria-hidden='true' />
                      ) : (
                        <Palette aria-hidden='true' />
                      )}
                      {useVisualEditor ? t('JSON Mode') : t('Visual Mode')}
                    </Button>
                  </div>
                  <FormDescription>
                    {t(
                      'The API key’s group takes precedence. The user’s group is used only if the key has no group. Limits apply to that user within the matched group and do not replace the user-level limits.'
                    )}
                  </FormDescription>
                  {useVisualEditor ? (
                    <RateLimitVisualEditor
                      value={field.value}
                      onChange={field.onChange}
                    />
                  ) : (
                    <>
                      <FormControl>
                        <JsonCodeEditor
                          value={field.value}
                          onChange={field.onChange}
                          name={field.name}
                          onBlur={field.onBlur}
                          textareaRef={field.ref}
                          placeholder={`{\n  "default": [200, 100],\n  "vip": [500, 0]\n}`}
                        />
                      </FormControl>
                      <p className='text-muted-foreground text-xs'>
                        {t('Format:')}{' '}
                        <code>
                          {'{"groupName": [maxRequests, maxSuccess]}'}
                        </code>
                        {' · '}
                        {t('0 means unlimited')}
                      </p>
                    </>
                  )}
                  <FormMessage />
                </FormItem>
              )}
            />
          </SettingsControlGroup>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
