import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
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

import { clearUserRequestLimit, setUserRequestLimit } from '../api'
import {
  createUserRequestLimitSchema,
  type UserRequestLimitFormValues,
} from '../lib/user-request-limit'
import type { UserRequestLimit } from '../types'

type UserRequestLimitDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  userId: number
  limit: UserRequestLimit
}

// Sets the request limits of one user. An empty field keeps the global default for
// that limit, which the server stores as 0.
export function UserRequestLimitDialog(props: UserRequestLimitDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const defaultValues = {
    total_count: props.limit.total_count,
    success_count: props.limit.success_count,
  }

  const form = useForm<UserRequestLimitFormValues>({
    resolver: zodResolver(createUserRequestLimitSchema(t)),
    mode: 'onChange',
    defaultValues,
  })

  const refresh = () => {
    queryClient.invalidateQueries({
      queryKey: ['user-request-limit', props.userId],
    })
  }

  const saveMutation = useMutation({
    mutationFn: (values: UserRequestLimitFormValues) =>
      setUserRequestLimit(props.userId, values),
    onSuccess: () => {
      toast.success(t('Request limits saved.'))
      refresh()
      props.onOpenChange(false)
    },
    onError: (error) =>
      handleServerError(error, t('Failed to save request limits.')),
  })

  const clearMutation = useMutation({
    mutationFn: () => clearUserRequestLimit(props.userId),
    onSuccess: () => {
      toast.success(t('Request limits removed.'))
      refresh()
      props.onOpenChange(false)
    },
    onError: (error) =>
      handleServerError(error, t('Failed to remove request limits.')),
  })

  const hasOverride =
    props.limit.total_count > 0 || props.limit.success_count > 0
  const fields = [
    {
      name: 'total_count' as const,
      label: t('Maximum requests per window'),
      defaultValue: props.limit.default_total,
    },
    {
      name: 'success_count' as const,
      label: t('Maximum successful requests per window'),
      defaultValue: props.limit.default_success,
    },
  ]

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Request limits')}
      description={t('Leave a field empty to use the global default for it.')}
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            disabled={!hasOverride || clearMutation.isPending}
            onClick={() => clearMutation.mutate()}
          >
            {t('Remove limits')}
          </Button>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            disabled={saveMutation.isPending}
            onClick={form.handleSubmit((values) => saveMutation.mutate(values))}
          >
            {t('Save')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <div className='grid gap-4'>
          {fields.map((item) => (
            <FormField
              key={item.name}
              control={form.control}
              name={item.name}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{item.label}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      inputMode='numeric'
                      min={0}
                      step={1}
                      name={field.name}
                      ref={field.ref}
                      value={field.value > 0 ? field.value : ''}
                      onBlur={field.onBlur}
                      onChange={(event) =>
                        field.onChange(
                          event.target.value === ''
                            ? 0
                            : event.target.valueAsNumber
                        )
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {item.defaultValue > 0
                      ? t('Global default: {{count}} per window.', {
                          count: item.defaultValue,
                        })
                      : t('The global default is off.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          ))}
        </div>
      </Form>
    </Dialog>
  )
}
