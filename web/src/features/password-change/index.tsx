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
import { useMutation } from '@tanstack/react-query'
import { useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { PasswordInput } from '@/components/password-input'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { clearAuthentication } from '@/lib/auth-session'
import { handleServerError } from '@/lib/handle-server-error'
import { accountPasswordSchema } from '@/lib/password-policy'

import { changeOwnPassword } from './api'

type ChangePasswordValues = {
  old_password: string
  new_password: string
  confirm_password: string
}

export function PasswordChange() {
  const { t } = useTranslation()

  const schema = useMemo(
    () =>
      z
        .object({
          old_password: z.string().min(1, t('Current password is required.')),
          new_password: accountPasswordSchema,
          confirm_password: z.string(),
        })
        .refine((values) => values.new_password === values.confirm_password, {
          path: ['confirm_password'],
          message: t('The passwords do not match.'),
        })
        .refine((values) => values.new_password !== values.old_password, {
          path: ['new_password'],
          message: t(
            'The new password must be different from the current password.'
          ),
        }),
    [t]
  )

  const form = useForm<ChangePasswordValues>({
    resolver: zodResolver(schema),
    defaultValues: { old_password: '', new_password: '', confirm_password: '' },
  })

  const mutation = useMutation({
    mutationFn: (values: ChangePasswordValues) =>
      changeOwnPassword(values.old_password, values.new_password),
    onSuccess: () => {
      toast.success(t('Password changed. Sign in again with the new password.'))
      // The server revokes every session on success, so the local state is
      // cleared and the user signs in again through the normal flow.
      clearAuthentication()
      window.location.replace('/sign-in')
    },
    onError: (error) =>
      handleServerError(error, t('Failed to change password.')),
  })

  return (
    <div className='mx-auto flex w-full max-w-md flex-col gap-4 p-4'>
      <Card>
        <CardHeader>
          <CardTitle>{t('Change your password')}</CardTitle>
          <CardDescription>
            {t('Your account requires a new password before you can continue.')}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Form {...form}>
            <form
              id='change-password-form'
              className='flex flex-col gap-4'
              onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
            >
              <FormField
                control={form.control}
                name='old_password'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Current password')}</FormLabel>
                    <FormControl>
                      <PasswordInput
                        {...field}
                        autoComplete='current-password'
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='new_password'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('New password')}</FormLabel>
                    <FormControl>
                      <PasswordInput {...field} autoComplete='new-password' />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='confirm_password'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Confirm new password')}</FormLabel>
                    <FormControl>
                      <PasswordInput {...field} autoComplete='new-password' />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <Button type='submit' disabled={mutation.isPending}>
                {t('Change password')}
              </Button>
            </form>
          </Form>
        </CardContent>
      </Card>
    </div>
  )
}
