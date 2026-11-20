import { useFormContext, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

import type { ChannelFormValues } from '../lib/channel-form'

// Edits the channel request budget. The two numeric fields appear only while the
// budget is enabled; their values stay in the form when it is switched off.
// Range checks come from the form schema, not native min/max, so the message is
// translated and shown next to the field instead of blocking submit silently.
export function ChannelRateLimitSetting(props: { disabled?: boolean }) {
  const { t } = useTranslation()
  const form = useFormContext<ChannelFormValues>()
  const enabled = useWatch({
    control: form.control,
    name: 'rate_limit.enabled',
  })

  return (
    <div className='space-y-4'>
      <FormField
        control={form.control}
        name='rate_limit.enabled'
        render={({ field }) => (
          <FormItem className='flex items-center justify-between gap-4 px-4 py-3'>
            <div className='space-y-0.5'>
              <FormLabel>{t('Request budget')}</FormLabel>
              <FormDescription>
                {t(
                  'Limit how many requests this channel accepts per window. Requests over the budget move to another channel of the same priority, or receive 429 when none has budget left.'
                )}
              </FormDescription>
            </div>
            <FormControl>
              <Switch
                checked={field.value === true}
                onCheckedChange={field.onChange}
                disabled={props.disabled}
                onBlur={field.onBlur}
                name={field.name}
                ref={field.ref}
              />
            </FormControl>
          </FormItem>
        )}
      />
      {enabled === true ? (
        <div className='grid gap-4 px-4 md:grid-cols-2'>
          <FormField
            control={form.control}
            name='rate_limit.requests'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Requests per window')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    inputMode='numeric'
                    disabled={props.disabled}
                    name={field.name}
                    ref={field.ref}
                    onBlur={field.onBlur}
                    value={field.value ?? ''}
                    onChange={(event) =>
                      field.onChange(
                        event.target.value === ''
                          ? undefined
                          : Number(event.target.value)
                      )
                    }
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='rate_limit.duration_seconds'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Window (seconds)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    inputMode='numeric'
                    disabled={props.disabled}
                    name={field.name}
                    ref={field.ref}
                    onBlur={field.onBlur}
                    value={field.value ?? ''}
                    onChange={(event) =>
                      field.onChange(
                        event.target.value === ''
                          ? undefined
                          : Number(event.target.value)
                      )
                    }
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>
      ) : null}
    </div>
  )
}
