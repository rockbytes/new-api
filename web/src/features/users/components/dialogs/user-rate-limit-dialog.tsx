import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useId } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
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
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { getUserRateLimit, updateUserRateLimit } from '../../api'
import {
  userRateLimitFormSchema,
  type UserRateLimitFormValues,
} from '../../lib/user-rate-limit'
import type { UserRateLimit } from '../../types'

interface DialogProps {
  userId: number
  username: string
  onClose: () => void
  onSaved: () => void
}

export function UserRateLimitDialog(props: DialogProps) {
  const { t } = useTranslation()
  const formId = useId()
  const queryClient = useQueryClient()
  const queryKey = ['user-rate-limit', props.userId]
  const query = useQuery({
    queryKey,
    queryFn: ({ signal }) => getUserRateLimit(props.userId, signal),
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    meta: { errorToast: false },
  })
  const limits =
    query.isSuccess && query.isFetchedAfterMount ? query.data : undefined
  const save = useMutation({
    mutationFn: (values: UserRateLimitFormValues) =>
      updateUserRateLimit(
        props.userId,
        values.mode === 'custom'
          ? { total: values.total, success: values.success }
          : null
      ),
    meta: { errorToast: false },
    onSuccess: () => {
      props.onClose()
      void queryClient.invalidateQueries({ queryKey, refetchType: 'none' })
      props.onSaved()
      toast.success(t('Rate limits saved'))
    },
  })

  let content = (
    <div role='status'>
      <LoadingState />
    </div>
  )
  if (limits) {
    content = (
      <UserRateLimitForm
        id={formId}
        limits={limits}
        saving={save.isPending}
        onSubmit={save.mutate}
      />
    )
  } else if (query.isError && !query.isFetching) {
    content = (
      <ErrorState
        title={t('Failed to load rate limits')}
        description={getServerErrorMessage(query.error, '')}
        onRetry={() => void query.refetch()}
      />
    )
  }

  return (
    <Dialog
      open
      onOpenChange={(isOpen) => !isOpen && !save.isPending && props.onClose()}
      title={t('Configure rate limit')}
      description={t(
        'All API keys and groups for {{username}} share the same user-level limits. Additional group limits still apply.',
        { username: props.username }
      )}
      contentClassName='sm:max-w-lg'
      showCloseButton={!save.isPending}
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            disabled={save.isPending}
            onClick={props.onClose}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='submit'
            form={formId}
            disabled={!limits || save.isPending}
          >
            {save.isPending ? t('Saving...') : t('Save')}
          </Button>
        </>
      }
    >
      {content}
      {save.isError && (
        <Alert variant='destructive' className='mt-4'>
          <AlertDescription>
            {getServerErrorMessage(save.error, t('Failed to save rate limits'))}
          </AlertDescription>
        </Alert>
      )}
    </Dialog>
  )
}

interface FormProps {
  id: string
  limits: UserRateLimit
  saving: boolean
  onSubmit: (values: UserRateLimitFormValues) => void
}

function UserRateLimitForm(props: FormProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const form = useForm<UserRateLimitFormValues>({
    defaultValues: {
      mode: props.limits.override ? 'custom' : 'inherit',
      ...(props.limits.override ?? props.limits.defaults),
    },
    resolver: zodResolver(userRateLimitFormSchema),
  })
  const mode = useWatch({ control: form.control, name: 'mode' })

  return (
    <Form {...form}>
      <form
        id={props.id}
        noValidate
        className='space-y-4'
        onSubmit={form.handleSubmit(props.onSubmit)}
      >
        {!props.limits.enabled && (
          <Alert>
            <AlertDescription>
              {t(
                'Saved limits are not enforced while model request rate limiting is off.'
              )}
            </AlertDescription>
          </Alert>
        )}
        <p className='text-muted-foreground text-sm'>
          {t('Limit period: {{minutes}} min', {
            minutes: formatNumber(props.limits.duration_minutes, locale),
          })}
        </p>
        <FormField
          control={form.control}
          name='mode'
          render={({ field }) => (
            <RadioGroup
              aria-label={t('User-level limit source')}
              value={field.value}
              onValueChange={field.onChange}
              disabled={props.saving}
            >
              <div className='flex items-center gap-2'>
                <RadioGroupItem value='inherit' id={`${props.id}-inherit`} />
                <Label htmlFor={`${props.id}-inherit`}>
                  {t('Inherit defaults')}
                </Label>
              </div>
              <div className='flex items-center gap-2'>
                <RadioGroupItem value='custom' id={`${props.id}-custom`} />
                <Label htmlFor={`${props.id}-custom`}>
                  {t('Custom limits')}
                </Label>
              </div>
            </RadioGroup>
          )}
        />
        {mode === 'inherit' ? (
          <section
            className='space-y-2'
            aria-label={t('Default limits per user')}
          >
            <p className='text-muted-foreground text-sm'>
              {t('Saving with this option removes any custom limits.')}
            </p>
            <dl className='grid grid-cols-[1fr_auto] gap-x-4 gap-y-2 text-sm'>
              <dt>{t('Max Requests (including failures)')}</dt>
              <dd>
                {props.limits.defaults.total === 0
                  ? t('Unlimited')
                  : formatNumber(props.limits.defaults.total, locale)}
              </dd>
              <dt>{t('Max Successful Requests')}</dt>
              <dd>
                {props.limits.defaults.success === 0
                  ? t('Unlimited')
                  : formatNumber(props.limits.defaults.success, locale)}
              </dd>
            </dl>
          </section>
        ) : (
          <div className='grid gap-4 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='total'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('Max Requests (including failures)')}
                  </FormLabel>
                  <FormControl>
                    <RateLimitCountInput {...field} disabled={props.saving} />
                  </FormControl>
                  <FormDescription>{t('0 means unlimited')}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='success'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Max Successful Requests')}</FormLabel>
                  <FormControl>
                    <RateLimitCountInput {...field} disabled={props.saving} />
                  </FormControl>
                  <FormDescription>{t('0 means unlimited')}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
        )}
      </form>
    </Form>
  )
}
