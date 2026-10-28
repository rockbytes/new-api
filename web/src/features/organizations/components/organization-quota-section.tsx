/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { handleServerError } from '@/lib/handle-server-error'

import {
  getOrganizationQuota,
  grantOrganizationQuota,
  refundOrganizationRequest,
} from '../api'

type OrganizationQuotaSectionProps = {
  organizationId: number
}

type GrantValues = { amount: string; reason: string; reference: string }
type RefundValues = { request_id: string; amount: string; reference: string }

// Root's view of an organization's quota: totals, a platform grant (D8), and a manual refund of a settled request (D10).
export function OrganizationQuotaSection(props: OrganizationQuotaSectionProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const organizationId = props.organizationId

  const quotaQuery = useQuery({
    queryKey: ['organization-quota', organizationId],
    queryFn: () => getOrganizationQuota(organizationId),
  })

  const refresh = () => {
    queryClient.invalidateQueries({
      queryKey: ['organization-quota', organizationId],
    })
    queryClient.invalidateQueries({
      queryKey: ['organization', organizationId],
    })
  }

  const grantSchema = useMemo(
    () =>
      z.object({
        amount: z
          .string()
          .trim()
          .regex(/^-?\d+$/, t('Enter a whole number of quota.'))
          .refine(
            (value) => Number(value) >= 1,
            t('Quota must be at least 1.')
          ),
        reason: z
          .string()
          .trim()
          .min(1, t('Enter a reason for this grant.'))
          .max(255, t('Reason is too long.')),
        reference: z
          .string()
          .trim()
          .min(1, t('Enter a reference for this grant.'))
          .max(191, t('Reference is too long.')),
      }),
    [t]
  )

  const refundSchema = useMemo(
    () =>
      z.object({
        request_id: z
          .string()
          .trim()
          .min(1, t('Enter the request ID.'))
          .max(64, t('Request ID is too long.')),
        amount: z
          .string()
          .trim()
          .regex(/^-?\d+$/, t('Enter a whole number of quota.'))
          .refine(
            (value) => Number(value) >= 1,
            t('Quota must be at least 1.')
          ),
        reference: z
          .string()
          .trim()
          .min(1, t('Enter a reference for this refund.'))
          .max(191, t('Reference is too long.')),
      }),
    [t]
  )

  const grantForm = useForm<GrantValues>({
    resolver: zodResolver(grantSchema),
    defaultValues: { amount: '', reason: '', reference: '' },
  })
  const refundForm = useForm<RefundValues>({
    resolver: zodResolver(refundSchema),
    defaultValues: { request_id: '', amount: '', reference: '' },
  })

  const grantMutation = useMutation({
    mutationFn: (values: GrantValues) =>
      grantOrganizationQuota(organizationId, {
        ...values,
        amount: Number(values.amount),
      }),
    onSuccess: () => {
      toast.success(t('Quota granted.'))
      grantForm.reset({ amount: '', reason: '', reference: '' })
      refresh()
    },
    onError: (error) => handleServerError(error, t('Failed to grant quota.')),
  })

  const refundMutation = useMutation({
    mutationFn: (values: RefundValues) =>
      refundOrganizationRequest(organizationId, {
        ...values,
        amount: Number(values.amount),
      }),
    onSuccess: () => {
      toast.success(t('Refund recorded.'))
      refundForm.reset({ request_id: '', amount: '', reference: '' })
      refresh()
    },
    onError: (error) =>
      handleServerError(error, t('Failed to refund the request.')),
  })

  const quota = quotaQuery.data

  return (
    <div className='flex flex-col gap-4'>
      <dl className='grid grid-cols-2 gap-3 text-sm sm:grid-cols-4'>
        <div>
          <dt className='text-muted-foreground'>{t('Granted quota')}</dt>
          <dd>{formatQuotaWithCurrency(quota?.granted_quota)}</dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>{t('Consumed quota')}</dt>
          <dd>{formatQuotaWithCurrency(quota?.consumed_quota)}</dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>{t('Remaining quota')}</dt>
          <dd>{formatQuotaWithCurrency(quota?.remaining_quota)}</dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>{t('Allocated to members')}</dt>
          <dd>{formatQuotaWithCurrency(quota?.allocated)}</dd>
        </div>
      </dl>

      <Form {...grantForm}>
        <form
          className='grid grid-cols-1 gap-3 sm:grid-cols-2'
          onSubmit={grantForm.handleSubmit((values) =>
            grantMutation.mutate(values)
          )}
        >
          <FormField
            control={grantForm.control}
            name='amount'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Grant amount')}</FormLabel>
                <FormControl>
                  <Input
                    type='text'
                    inputMode='numeric'
                    name={field.name}
                    ref={field.ref}
                    onBlur={field.onBlur}
                    value={field.value}
                    onChange={field.onChange}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={grantForm.control}
            name='reason'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Reason')}</FormLabel>
                <FormControl>
                  <Input {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={grantForm.control}
            name='reference'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Reference')}</FormLabel>
                <FormControl>
                  <Input {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <div className='flex items-end'>
            <Button type='submit' disabled={grantMutation.isPending}>
              {t('Grant quota')}
            </Button>
          </div>
        </form>
      </Form>

      <Form {...refundForm}>
        <form
          className='grid grid-cols-1 gap-3 sm:grid-cols-2'
          onSubmit={refundForm.handleSubmit((values) =>
            refundMutation.mutate(values)
          )}
        >
          <FormField
            control={refundForm.control}
            name='request_id'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Request ID')}</FormLabel>
                <FormControl>
                  <Input {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={refundForm.control}
            name='amount'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Refund amount')}</FormLabel>
                <FormControl>
                  <Input
                    type='text'
                    inputMode='numeric'
                    name={field.name}
                    ref={field.ref}
                    onBlur={field.onBlur}
                    value={field.value}
                    onChange={field.onChange}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={refundForm.control}
            name='reference'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Reference')}</FormLabel>
                <FormControl>
                  <Input {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <div className='flex items-end'>
            <Button
              type='submit'
              variant='outline'
              disabled={refundMutation.isPending}
            >
              {t('Refund request')}
            </Button>
          </div>
        </form>
      </Form>
    </div>
  )
}
