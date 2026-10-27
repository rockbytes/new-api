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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useMemo } from 'react'
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
import { handleServerError } from '@/lib/handle-server-error'
import { accountPasswordSchema } from '@/lib/password-policy'

import {
  createOrganizationMember,
  resetOrganizationMemberPassword,
} from '../api'
import type { OrganizationMemberRow } from '../types'

type CreateMemberValues = { username: string; password: string }
type ResetPasswordValues = { password: string }

type SheetProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function CreateMemberSheet(props: SheetProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const schema = useMemo(
    () =>
      z.object({
        username: z.string().trim().min(1, t('Username is required.')),
        password: accountPasswordSchema,
      }),
    [t]
  )
  const form = useForm<CreateMemberValues>({
    resolver: zodResolver(schema),
    defaultValues: { username: '', password: '' },
  })

  const mutation = useMutation({
    mutationFn: createOrganizationMember,
    onSuccess: () => {
      toast.success(t('Member account created.'))
      queryClient.invalidateQueries({ queryKey: ['organization-members'] })
      form.reset()
      props.onOpenChange(false)
    },
    onError: (error) =>
      handleServerError(error, t('Failed to create member account.')),
  })

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className={sideDrawerContentClassName()}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>{t('Create member account')}</SheetTitle>
          <SheetDescription>
            {t(
              'The member signs in with the initial password and must change it at first login.'
            )}
          </SheetDescription>
        </SheetHeader>
        <Form {...form}>
          <form
            id='create-member-form'
            className={sideDrawerFormClassName()}
            onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
          >
            <FormField
              control={form.control}
              name='username'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Username')}</FormLabel>
                  <FormControl>
                    <Input {...field} autoComplete='off' />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='password'
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
          </form>
        </Form>
        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose render={<Button variant='outline' />}>
            {t('Cancel')}
          </SheetClose>
          <Button
            type='submit'
            form='create-member-form'
            disabled={mutation.isPending}
          >
            {t('Create')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

type ResetPasswordSheetProps = SheetProps & {
  member: OrganizationMemberRow | null
}

export function ResetMemberPasswordSheet(props: ResetPasswordSheetProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const memberId = props.member?.user_id ?? 0

  const form = useForm<ResetPasswordValues>({
    resolver: zodResolver(z.object({ password: accountPasswordSchema })),
    defaultValues: { password: '' },
  })

  const mutation = useMutation({
    mutationFn: (values: ResetPasswordValues) =>
      resetOrganizationMemberPassword(memberId, values.password),
    onSuccess: () => {
      toast.success(
        t('Password reset. The member must change it at next login.')
      )
      queryClient.invalidateQueries({ queryKey: ['organization-members'] })
      form.reset()
      props.onOpenChange(false)
    },
    onError: (error) =>
      handleServerError(error, t('Failed to reset password.')),
  })

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className={sideDrawerContentClassName()}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>{t('Reset member password')}</SheetTitle>
          <SheetDescription>
            {t(
              'Sets a new initial password for {{username}}. Their current sessions end, and they must change the password at next login.',
              { username: props.member?.username ?? '' }
            )}
          </SheetDescription>
        </SheetHeader>
        <Form {...form}>
          <form
            id='reset-member-password-form'
            className={sideDrawerFormClassName()}
            onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
          >
            <FormField
              control={form.control}
              name='password'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('New initial password')}</FormLabel>
                  <FormControl>
                    <PasswordInput {...field} autoComplete='new-password' />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </form>
        </Form>
        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose render={<Button variant='outline' />}>
            {t('Cancel')}
          </SheetClose>
          <Button
            type='submit'
            form='reset-member-password-form'
            disabled={mutation.isPending}
          >
            {t('Reset password')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
