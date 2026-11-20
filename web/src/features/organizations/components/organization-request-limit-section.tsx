import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { sideDrawerSectionClassName } from '@/components/drawer-layout'
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
import { handleServerError } from '@/lib/handle-server-error'

import {
  clearOrganizationRequestLimit,
  getOrganizationRequestLimits,
  setOrganizationRequestLimit,
} from '../api'
import {
  createRequestLimitSchema,
  type RequestLimitFormValues,
} from '../lib/request-limit-schema'

type OrganizationRequestLimitSectionProps = {
  organizationId: number
}

// Root sets an organization's total request limit here. The limit covers the
// requests paid by that organization, counted per rate-limit window.
export function OrganizationRequestLimitSection(
  props: OrganizationRequestLimitSectionProps
) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const organizationId = props.organizationId

  const limitsQuery = useQuery({
    queryKey: ['rate-limit-overrides', 'organization'],
    queryFn: getOrganizationRequestLimits,
  })
  const currentLimit =
    limitsQuery.data?.find((item) => item.subject_id === organizationId)
      ?.total_count ?? 0

  const form = useForm<RequestLimitFormValues>({
    resolver: zodResolver(createRequestLimitSchema(t)),
    mode: 'onChange',
    defaultValues: { total_count: currentLimit },
  })

  useEffect(() => {
    form.reset({ total_count: currentLimit })
  }, [currentLimit, form])

  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: ['rate-limit-overrides'] })
  }

  const saveMutation = useMutation({
    mutationFn: (value: number) =>
      setOrganizationRequestLimit(organizationId, value),
    onSuccess: () => {
      toast.success(t('Request limit saved.'))
      refresh()
    },
    onError: (error) =>
      handleServerError(error, t('Failed to save request limit.')),
  })

  const clearMutation = useMutation({
    mutationFn: () => clearOrganizationRequestLimit(organizationId),
    onSuccess: () => {
      toast.success(t('Request limit removed.'))
      refresh()
    },
    onError: (error) =>
      handleServerError(error, t('Failed to remove request limit.')),
  })

  const statusText =
    currentLimit > 0
      ? t('Current limit: {{count}} requests per window.', {
          count: currentLimit,
        })
      : t('No request limit is set for this organization.')

  return (
    <section className={sideDrawerSectionClassName()}>
      <h3 className='text-sm font-medium'>{t('Request limit')}</h3>
      <p className='text-muted-foreground text-sm'>{statusText}</p>
      <Form {...form}>
        <form
          className='flex flex-wrap items-start gap-2'
          onSubmit={form.handleSubmit((values) =>
            saveMutation.mutate(values.total_count)
          )}
        >
          <FormField
            control={form.control}
            name='total_count'
            render={({ field }) => (
              <FormItem className='min-w-48 flex-1'>
                <FormLabel>{t('Maximum requests per window')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={1}
                    step={1}
                    name={field.name}
                    ref={field.ref}
                    value={Number.isNaN(field.value) ? '' : field.value}
                    onBlur={field.onBlur}
                    onChange={(event) =>
                      field.onChange(event.target.valueAsNumber)
                    }
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'The window is set in the rate limit settings. The limit applies only to requests this organization pays for.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <div className='flex gap-2 pt-7'>
            <Button type='submit' disabled={saveMutation.isPending}>
              {t('Save')}
            </Button>
            <Button
              type='button'
              variant='outline'
              disabled={currentLimit === 0 || clearMutation.isPending}
              onClick={() => clearMutation.mutate()}
            >
              {t('Remove limit')}
            </Button>
          </div>
        </form>
      </Form>
    </section>
  )
}
