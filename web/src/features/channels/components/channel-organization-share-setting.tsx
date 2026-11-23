import { useQuery } from '@tanstack/react-query'
import { Plus, X } from 'lucide-react'
import { useFieldArray, useFormContext } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import {
  FormControl,
  FormField,
  FormItem,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { getOrganizations } from '@/features/organizations/api'

import type { ChannelFormValues } from '../lib/channel-form'

// Edits the organization shares of the request budget. Each row caps one organization
// at a percentage of the budget; an organization without a row is limited only by the
// budget itself.
export function ChannelOrganizationShareSetting(props: { disabled?: boolean }) {
  const { t } = useTranslation()
  const form = useFormContext<ChannelFormValues>()
  const shares = useFieldArray({
    control: form.control,
    name: 'rate_limit.org_shares',
  })

  const organizations = useQuery({
    queryKey: ['channels', 'organization-options'],
    queryFn: () => getOrganizations(1, 100),
    staleTime: 5 * 60 * 1000,
  })
  const organizationOptions = (organizations.data?.items ?? []).map(
    (organization) => ({
      value: String(organization.id),
      label: organization.name,
    })
  )

  return (
    <div className='space-y-3'>
      <div className='space-y-0.5'>
        <p className='text-sm leading-none font-medium'>
          {t('Organization shares')}
        </p>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Cap how much of the request budget one organization can use, as a percentage of the requests per window. Organizations without a share are limited only by the budget.'
          )}
        </p>
      </div>
      {shares.fields.map((row, index) => (
        <div
          key={row.id}
          className='grid gap-2 md:grid-cols-[1fr_8rem_auto] md:items-start'
        >
          <FormField
            control={form.control}
            name={`rate_limit.org_shares.${index}.organization_id`}
            render={({ field }) => (
              <FormItem>
                <FormControl>
                  <Combobox
                    aria-label={t('Organization')}
                    options={organizationOptions}
                    value={field.value ? String(field.value) : null}
                    onValueChange={(value) =>
                      field.onChange(value ? Number(value) : undefined)
                    }
                    placeholder={t('Select an organization')}
                    className='w-full'
                    disabled={props.disabled}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name={`rate_limit.org_shares.${index}.percent`}
            render={({ field }) => (
              <FormItem>
                <FormControl>
                  <Input
                    type='number'
                    inputMode='numeric'
                    aria-label={t('Share (%)')}
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
          <Button
            type='button'
            variant='ghost'
            size='icon'
            aria-label={t('Remove organization share')}
            disabled={props.disabled}
            onClick={() => shares.remove(index)}
          >
            <X />
          </Button>
        </div>
      ))}
      <Button
        type='button'
        variant='outline'
        disabled={props.disabled}
        onClick={() => shares.append({})}
      >
        <Plus />
        {t('Add organization share')}
      </Button>
    </div>
  )
}
