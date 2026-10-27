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

import { createOrganization } from '../api'
import type { CreateOrganizationPayload } from '../types'

type CreateOrganizationSheetProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function CreateOrganizationSheet(props: CreateOrganizationSheetProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const formSchema = useMemo(
    () =>
      z.object({
        name: z
          .string()
          .trim()
          .min(1, t('Organization name is required.'))
          .max(128, t('Organization name is too long.')),
        admin_username: z
          .string()
          .trim()
          .min(1, t('Administrator username is required.')),
        admin_password: accountPasswordSchema,
      }),
    [t]
  )

  const form = useForm<CreateOrganizationPayload>({
    resolver: zodResolver(formSchema),
    defaultValues: { name: '', admin_username: '', admin_password: '' },
  })

  const createMutation = useMutation({
    mutationFn: createOrganization,
    onSuccess: () => {
      toast.success(t('Organization created.'))
      queryClient.invalidateQueries({ queryKey: ['organizations'] })
      form.reset()
      props.onOpenChange(false)
    },
    onError: (error) =>
      handleServerError(error, t('Failed to create organization.')),
  })

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className={sideDrawerContentClassName()}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>{t('Create organization')}</SheetTitle>
          <SheetDescription>
            {t(
              'Creates the organization and its first admin account. The admin signs in with the initial password and must change it at first login.'
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
