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
import { useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import {
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { PasswordInput } from '@/components/password-input'
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
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { searchUsers } from '@/features/users/api'
import { handleServerError } from '@/lib/handle-server-error'
import { accountPasswordSchema } from '@/lib/password-policy'
import { ROLE } from '@/lib/roles'

import { createOrganization } from '../api'
import type { CreateOrganizationPayload } from '../types'

type CreateOrganizationSheetProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

// Either a new account is created as the first admin, or an existing eligible user is chosen.
type AdminMode = 'new' | 'existing'

type CreateOrganizationFormValues = {
  name: string
  admin_mode: AdminMode
  admin_username: string
  admin_password: string
  admin_user_id?: number
}

export function CreateOrganizationSheet(props: CreateOrganizationSheetProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [keyword, setKeyword] = useState('')

  const formSchema = useMemo(
    () =>
      z
        .object({
          name: z
            .string()
            .trim()
            .min(1, t('Organization name is required.'))
            .max(128, t('Organization name is too long.')),
          admin_mode: z.enum(['new', 'existing']),
          admin_username: z.string().trim(),
          admin_password: z.string(),
          admin_user_id: z.number().optional(),
        })
        .superRefine((values, ctx) => {
          if (values.admin_mode === 'existing') {
            if (!values.admin_user_id) {
              ctx.addIssue({
                code: 'custom',
                path: ['admin_user_id'],
                message: t('Select an existing user as the first admin.'),
              })
            }
            return
          }
          if (!values.admin_username) {
            ctx.addIssue({
              code: 'custom',
              path: ['admin_username'],
              message: t('Administrator username is required.'),
            })
          }
          const password = accountPasswordSchema.safeParse(
            values.admin_password
          )
          if (!password.success) {
            ctx.addIssue({
              code: 'custom',
              path: ['admin_password'],
              message: password.error.issues[0]?.message ?? '',
            })
          }
        }),
    [t]
  )

  const form = useForm<CreateOrganizationFormValues>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      name: '',
      admin_mode: 'new',
      admin_username: '',
      admin_password: '',
      admin_user_id: undefined,
    },
  })
  const adminMode = form.watch('admin_mode')
  const selectedUserId = form.watch('admin_user_id')

  // Only enabled common users who belong to no organization can become the first admin.
  const candidatesQuery = useQuery({
    queryKey: ['organization-admin-candidates', keyword],
    queryFn: () =>
      searchUsers({
        keyword,
        role: String(ROLE.USER),
        organization_id: '0',
        status: '1',
        page_size: 20,
      }),
    enabled: adminMode === 'existing',
  })
  const candidates = candidatesQuery.data?.data?.items ?? []

  const createMutation = useMutation({
    mutationFn: (values: CreateOrganizationFormValues) => {
      const payload: CreateOrganizationPayload =
        values.admin_mode === 'existing'
          ? { name: values.name, admin_user_id: values.admin_user_id }
          : {
              name: values.name,
              admin_username: values.admin_username,
              admin_password: values.admin_password,
            }
      return createOrganization(payload)
    },
    onSuccess: () => {
      toast.success(t('Organization created.'))
      queryClient.invalidateQueries({ queryKey: ['organizations'] })
      form.reset()
      setKeyword('')
      props.onOpenChange(false)
    },
    onError: (error) =>
      handleServerError(error, t('Failed to create organization.')),
  })

  const switchMode = (mode: AdminMode) => {
    form.setValue('admin_mode', mode)
    form.clearErrors()
  }

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className={sideDrawerContentClassName()}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>{t('Create organization')}</SheetTitle>
          <SheetDescription>
            {t(
              'Creates the organization and its first admin. A new account signs in with the initial password and must change it at first login.'
            )}
          </SheetDescription>
        </SheetHeader>
        <Form {...form}>
          <form
            id='create-organization-form'
            className={sideDrawerFormClassName()}
            onSubmit={form.handleSubmit((values) =>
              createMutation.mutate(values)
            )}
          >
            <FormField
              control={form.control}
              name='name'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Organization name')}</FormLabel>
                  <FormControl>
                    <Input {...field} autoComplete='off' />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <div className='flex flex-col gap-2'>
              <span className='text-sm font-medium'>{t('First admin')}</span>
              <div className='flex gap-2'>
                <Button
                  type='button'
                  size='sm'
                  variant={adminMode === 'new' ? 'default' : 'outline'}
                  onClick={() => switchMode('new')}
                >
                  {t('Create a new account')}
                </Button>
                <Button
                  type='button'
                  size='sm'
                  variant={adminMode === 'existing' ? 'default' : 'outline'}
                  onClick={() => switchMode('existing')}
                >
                  {t('Use an existing user')}
                </Button>
              </div>
            </div>

            {adminMode === 'new' ? (
              <>
                <FormField
                  control={form.control}
                  name='admin_username'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('First admin username')}</FormLabel>
                      <FormControl>
                        <Input {...field} autoComplete='off' />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='admin_password'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Initial password')}</FormLabel>
                      <FormControl>
                        <PasswordInput {...field} autoComplete='new-password' />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </>
            ) : (
              <FormField
                control={form.control}
                name='admin_user_id'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Select a user')}</FormLabel>
                    <FormControl>
                      <Input
                        value={keyword}
                        placeholder={t('Filter by username, name or email...')}
                        onChange={(event) => setKeyword(event.target.value)}
                        autoComplete='off'
                      />
                    </FormControl>
                    <p className='text-muted-foreground text-xs'>
                      {t(
                        'Only enabled users who belong to no organization can be chosen.'
                      )}
                    </p>
                    <div className='flex flex-col gap-1'>
                      {candidates.length === 0 &&
                      !candidatesQuery.isFetching ? (
                        <p className='text-muted-foreground text-sm'>
                          {t('No eligible users found.')}
                        </p>
                      ) : null}
                      {candidates.map((user) => (
                        <Button
                          key={user.id}
                          type='button'
                          size='sm'
                          variant={
                            selectedUserId === user.id ? 'default' : 'outline'
                          }
                          className='justify-start'
                          onClick={() => field.onChange(user.id)}
                        >
                          {user.username}
                          {user.display_name &&
                          user.display_name !== user.username
                            ? ` (${user.display_name})`
                            : ''}
                        </Button>
                      ))}
                    </div>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}
          </form>
        </Form>
        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose render={<Button variant='outline' />}>
            {t('Cancel')}
          </SheetClose>
          <Button
            type='submit'
            form='create-organization-form'
            disabled={createMutation.isPending}
          >
            {t('Create')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
