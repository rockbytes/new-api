import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
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
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { getGroups } from '@/features/users/api'
import { handleServerError } from '@/lib/handle-server-error'
import { accountPasswordSchema } from '@/lib/password-policy'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  createOrganizationMember,
  getUsableGroupNames,
  resetOrganizationMemberPassword,
} from '../api'
import type { OrganizationMemberRow, OrganizationRole } from '../types'

export type CreateMemberValues = {
  username: string
  password: string
  group: string
  role: OrganizationRole
}
type ResetPasswordValues = { password: string }

type SheetProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

type CreateMemberSheetProps = SheetProps & {
  // Creates the account. Organization admins create it in their own organization, which is the default.
  // Platform administrators pass the organization they are viewing.
  submit?: (values: CreateMemberValues) => Promise<void>
  // Shows the user group picker. Options come from groupScope.
  withGroupOption?: boolean
  // 'all' lists every configured group (platform administrators); 'usable' lists only
  // the groups the signed-in user may select (organization admins).
  groupScope?: 'all' | 'usable'
  // Platform administrators also choose the organization role.
  withRoleOption?: boolean
  onCreated?: () => void
}

const MEMBER_ROLE_OPTIONS: OrganizationRole[] = ['member', 'admin']

export function CreateMemberSheet(props: CreateMemberSheetProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const showGroup = props.withGroupOption === true
  const showRole = props.withRoleOption === true
  const groupScope = props.groupScope ?? 'usable'

  const schema = useMemo(
    () =>
      z.object({
        username: z.string().trim().min(1, t('Username is required.')),
        password: accountPasswordSchema,
        group: z
          .string()
          .refine(
            (value) => !showGroup || value.trim() !== '',
            t('Select a group')
          ),
        role: z.enum(['admin', 'member']),
      }),
    [t, showGroup]
  )
  const form = useForm<CreateMemberValues>({
    resolver: zodResolver(schema),
    defaultValues: { username: '', password: '', group: '', role: 'member' },
  })

  const allGroupsQuery = useQuery({
    queryKey: ['groups'],
    queryFn: async () => requireServerSuccess(await getGroups()),
    enabled: showGroup && groupScope === 'all',
  })
  const usableGroupsQuery = useQuery({
    queryKey: ['organization-usable-groups'],
    queryFn: getUsableGroupNames,
    enabled: showGroup && groupScope === 'usable',
  })
  const groups =
    groupScope === 'all'
      ? (allGroupsQuery.data?.data ?? [])
      : (usableGroupsQuery.data ?? [])

  const mutation = useMutation({
    mutationFn: (values: CreateMemberValues) => {
      if (props.submit) {
        return props.submit(values)
      }
      return createOrganizationMember({
        username: values.username,
        password: values.password,
        ...(showGroup ? { group: values.group } : {}),
      })
    },
    onSuccess: () => {
      toast.success(t('Member account created.'))
      queryClient.invalidateQueries({ queryKey: ['organization-members'] })
      props.onCreated?.()
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
            {showGroup ? (
              <FormField
                control={form.control}
                name='group'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('User group')}</FormLabel>
                    <Select
                      items={groups.map((group) => ({
                        value: group,
                        label: group,
                      }))}
                      onValueChange={(value) =>
                        value !== null && field.onChange(value)
                      }
                      value={field.value}
                    >
                      <FormControl>
                        <SelectTrigger>
                          <SelectValue placeholder={t('Select a group')} />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {groups.map((group) => (
                            <SelectItem key={group} value={group}>
                              {group}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FormMessage />
                  </FormItem>
                )}
              />
            ) : null}
            {showRole ? (
              <FormField
                control={form.control}
                name='role'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Organization role')}</FormLabel>
                    <Select
                      items={MEMBER_ROLE_OPTIONS.map((role) => ({
                        value: role,
                        label: role === 'admin' ? t('Admin') : t('Member'),
                      }))}
                      onValueChange={(value) =>
                        value !== null && field.onChange(value)
                      }
                      value={field.value}
                    >
                      <FormControl>
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {MEMBER_ROLE_OPTIONS.map((role) => (
                            <SelectItem key={role} value={role}>
                              {role === 'admin' ? t('Admin') : t('Member')}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FormMessage />
                  </FormItem>
                )}
              />
            ) : null}
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
