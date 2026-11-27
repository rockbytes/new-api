import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { Dialog } from '@/components/dialog'
import { RateLimitCountInput } from '@/components/rate-limit-count-input'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
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
import { getUserGroups } from '@/lib/api'
import { rateLimitCountSchema } from '@/lib/rate-limits'
import { requireServerSuccess } from '@/lib/server-error-message'

const rateLimitDialogSchema = z.object({
  groupName: z.string().min(1, 'Select a request group.'),
  maxRequests: rateLimitCountSchema,
  maxSuccess: rateLimitCountSchema,
})

type RateLimitDialogFormValues = z.infer<typeof rateLimitDialogSchema>

const RATE_LIMIT_FORM_ID = 'rate-limit-form'

export type RateLimitEntryData = {
  groupName: string
  maxRequests: number
  maxSuccess: number
}

type RateLimitDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (data: RateLimitEntryData) => void
  editData?: RateLimitEntryData | null
  configuredGroups: readonly string[]
}

export function RateLimitDialog({
  open,
  onOpenChange,
  onSave,
  editData,
  configuredGroups,
}: RateLimitDialogProps) {
  const { t } = useTranslation()
  const isEditMode = !!editData
  const groups = useQuery({
    queryKey: ['user-groups'],
    queryFn: async () => requireServerSuccess(await getUserGroups()),
    enabled: open && !isEditMode,
    meta: { errorToast: false },
  })
  const groupOptions = Object.keys(groups.data?.data ?? {})
    .filter((name) => !configuredGroups.includes(name))
    .sort((a, b) => a.localeCompare(b))
    .map((name) => ({ value: name, label: name }))
  const canChooseGroup = groups.isSuccess && groupOptions.length > 0

  const form = useForm<RateLimitDialogFormValues>({
    resolver: zodResolver(rateLimitDialogSchema),
    defaultValues: {
      groupName: '',
      maxRequests: 0,
      maxSuccess: 0,
    },
  })

  useEffect(() => {
    if (editData) {
      form.reset(editData)
    } else {
      form.reset({
        groupName: '',
        maxRequests: 0,
        maxSuccess: 0,
      })
    }
  }, [editData, form, open])

  const handleSubmit = (values: RateLimitDialogFormValues) => {
    if (
      !isEditMode &&
      !groupOptions.some((option) => option.value === values.groupName)
    ) {
      form.setError('groupName', { message: 'Select a request group.' })
      return
    }
    onSave(values)
    form.reset()
    onOpenChange(false)
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={
        isEditMode ? t('Edit group rate limit') : t('Add group rate limit')
      }
      description={t('Set additional request limits per user in this group.')}
      contentClassName='sm:max-w-[500px]'
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='submit'
            form={RATE_LIMIT_FORM_ID}
            disabled={!isEditMode && !canChooseGroup}
          >
            {isEditMode ? t('Update') : t('Add')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id={RATE_LIMIT_FORM_ID}
          noValidate
          onSubmit={(event) => {
            event.stopPropagation()
            void form.handleSubmit(handleSubmit)(event)
          }}
          className='space-y-4'
        >
          <FormField
            control={form.control}
            name='groupName'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Group Name')}</FormLabel>
                {isEditMode ? (
                  <FormControl>
                    <Input {...field} readOnly />
                  </FormControl>
                ) : (
                  <FormControl>
                    <Combobox
                      options={groupOptions}
                      value={field.value}
                      onValueChange={(value) => field.onChange(value ?? '')}
                      onBlur={field.onBlur}
                      name={field.name}
                      ref={field.ref}
                      placeholder={t('Search groups...')}
                      emptyText={t('No groups match your search')}
                      disabled={!canChooseGroup}
                    />
                  </FormControl>
                )}
                {isEditMode && (
                  <FormDescription>
                    {t('Group name cannot be changed when editing.')}
                  </FormDescription>
                )}
                {!isEditMode && groups.isPending && (
                  <FormDescription role='status'>
                    {t('Loading...')}
                  </FormDescription>
                )}
                {!isEditMode && groups.isError && (
                  <div className='flex items-center gap-2'>
                    <FormDescription role='alert'>
                      {t('Failed to load groups')}
                    </FormDescription>
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      disabled={groups.isFetching}
                      onClick={() => void groups.refetch()}
                    >
                      {t('Retry')}
                    </Button>
                  </div>
                )}
                {!isEditMode &&
                  groups.isSuccess &&
                  groupOptions.length === 0 && (
                    <FormDescription>
                      {t('No unconfigured request groups available.')}
                    </FormDescription>
                  )}
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='maxRequests'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Max Requests (including failures)')}</FormLabel>
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
            name='maxSuccess'
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
        </form>
      </Form>
    </Dialog>
  )
}
