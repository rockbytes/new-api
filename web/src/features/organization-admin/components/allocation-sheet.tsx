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
import { useEffect, useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import {
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
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
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { formatQuotaWithCurrency } from '@/lib/currency'

type AllocationSheetProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  username: string
  allocated: number
  used: number
  isPending: boolean
  onSubmit: (allocated: number) => void
}

type AllocationValues = { allocated: string }

// Sets a member's allocation: the limit on the member's usage. It is not a transfer.
export function AllocationSheet(props: AllocationSheetProps) {
  const { t } = useTranslation()

  // The field keeps the text as typed, so a leading minus sign is not lost, and the
  // value is converted to a number only when it is submitted.
  const schema = useMemo(
    () =>
      z.object({
        allocated: z
          .string()
          .trim()
          .regex(/^-?\d+$/, t('Enter a whole number of quota.'))
          .refine(
            (value) => Number(value) >= 0,
            t('Quota cannot be negative.')
          ),
      }),
    [t]
  )

  const form = useForm<AllocationValues>({
    resolver: zodResolver(schema),
    defaultValues: { allocated: String(props.allocated) },
  })

  useEffect(() => {
    if (props.open) {
      form.reset({ allocated: String(props.allocated) })
    }
  }, [props.open, props.allocated, form])

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className={sideDrawerContentClassName()}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>{t('Set member allocation')}</SheetTitle>
          <SheetDescription>
            {t(
              'The allocation limits how much this member can use. It does not move quota from the organization.'
            )}
          </SheetDescription>
        </SheetHeader>
        <Form {...form}>
          <form
            id='allocation-form'
            className={sideDrawerFormClassName()}
            onSubmit={form.handleSubmit((values) =>
              props.onSubmit(Number(values.allocated))
            )}
          >
            <p className='text-sm'>
              {t('Member')}: {props.username}
            </p>
            <p className='text-muted-foreground text-sm'>
              {t('Used quota')}: {formatQuotaWithCurrency(props.used)}
            </p>
            <FormField
              control={form.control}
              name='allocated'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Allocated quota')}</FormLabel>
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
                  <FormDescription>
                    {t('Quota in raw units, the same unit used by the API.')}
                  </FormDescription>
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
            form='allocation-form'
            disabled={props.isPending}
          >
            {t('Save')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
