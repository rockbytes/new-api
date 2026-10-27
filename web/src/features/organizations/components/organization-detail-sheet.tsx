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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import {
  sideDrawerContentClassName,
  sideDrawerHeaderClassName,
  sideDrawerSectionClassName,
} from '@/components/drawer-layout'
import { StatusBadge, statusPresets } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { formatTimestamp } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import {
  getOrganizationDetail,
  updateOrganizationName,
  updateOrganizationStatus,
} from '../api'
import type { OrganizationMember, OrganizationStatus } from '../types'

type OrganizationDetailSheetProps = {
  organizationId: number | null
  onOpenChange: (open: boolean) => void
}

export function OrganizationDetailSheet(props: OrganizationDetailSheetProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const organizationId = props.organizationId
  const [name, setName] = useState('')
  const [confirmSuspend, setConfirmSuspend] = useState(false)

  const detailQuery = useQuery({
    queryKey: ['organization', organizationId],
    queryFn: () => getOrganizationDetail(organizationId ?? 0),
    enabled: organizationId !== null,
  })
  const organization = detailQuery.data?.organization

  useEffect(() => {
    if (organization) setName(organization.name)
  }, [organization])

  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: ['organizations'] })
    queryClient.invalidateQueries({
      queryKey: ['organization', organizationId],
    })
  }

  const renameMutation = useMutation({
    mutationFn: (value: string) =>
      updateOrganizationName(organizationId ?? 0, value),
    onSuccess: () => {
      toast.success(t('Organization updated.'))
      refresh()
    },
    onError: (error) =>
      handleServerError(error, t('Failed to update organization.')),
  })

  const statusMutation = useMutation({
    mutationFn: (status: OrganizationStatus) =>
      updateOrganizationStatus(organizationId ?? 0, status),
    onSuccess: () => {
      toast.success(t('Organization status updated.'))
      setConfirmSuspend(false)
      refresh()
    },
    onError: (error) =>
      handleServerError(error, t('Failed to update organization status.')),
  })

  const nextStatus: OrganizationStatus =
    organization?.status === 'active' ? 'suspended' : 'active'

  const memberColumns: StaticDataTableColumn<OrganizationMember>[] = [
    {
      id: 'username',
      header: t('Username'),
      cell: (row) => row.username,
    },
    {
      id: 'role',
      header: t('Role'),
      cell: (row) => (row.role === 'admin' ? t('Admin') : t('Member')),
    },
    {
      id: 'status',
      header: t('Status'),
      cell: (row) =>
        row.status === 'active' ? (
          <StatusBadge {...statusPresets.active} />
        ) : (
          <StatusBadge {...statusPresets.inactive} />
        ),
    },
    {
      id: 'created',
      header: t('Created'),
      cell: (row) => formatTimestamp(row.created_time),
    },
  ]

  return (
    <>
      <Sheet
        open={organizationId !== null}
        onOpenChange={(open) => !open && props.onOpenChange(false)}
      >
        <SheetContent className={sideDrawerContentClassName('sm:max-w-2xl')}>
          <SheetHeader className={sideDrawerHeaderClassName()}>
            <SheetTitle>{organization?.name ?? t('Organization')}</SheetTitle>
            <SheetDescription>
              {t(
                'Rename the organization, suspend or resume it, and review its members.'
              )}
            </SheetDescription>
          </SheetHeader>
          {organization ? (
            <div className='flex flex-col gap-6 overflow-y-auto p-4'>
              <section className={sideDrawerSectionClassName()}>
                <label
                  htmlFor='organization-name'
                  className='text-sm font-medium'
                >
                  {t('Organization name')}
                </label>
                <div className='flex gap-2'>
                  <Input
                    id='organization-name'
                    value={name}
                    onChange={(event) => setName(event.target.value)}
                  />
                  <Button
                    onClick={() => renameMutation.mutate(name.trim())}
                    disabled={
                      renameMutation.isPending ||
                      name.trim() === '' ||
                      name.trim() === organization.name
                    }
                  >
                    {t('Save')}
                  </Button>
                </div>
              </section>
              <section className={sideDrawerSectionClassName()}>
                <div className='flex items-center justify-between gap-2'>
                  <span className='text-sm font-medium'>{t('Status')}</span>
                  <div className='flex items-center gap-2'>
                    <StatusBadge
                      label={
                        organization.status === 'active'
                          ? t('Active')
                          : t('Suspended')
                      }
                      variant={
                        organization.status === 'active' ? 'success' : 'danger'
                      }
                    />
                    <Button
                      variant='outline'
                      onClick={() =>
                        nextStatus === 'suspended'
                          ? setConfirmSuspend(true)
                          : statusMutation.mutate(nextStatus)
                      }
                      disabled={statusMutation.isPending}
                    >
                      {nextStatus === 'suspended' ? t('Suspend') : t('Resume')}
                    </Button>
                  </div>
                </div>
              </section>
              <section className={sideDrawerSectionClassName()}>
                <h3 className='text-sm font-medium'>{t('Members')}</h3>
                <StaticDataTable
                  columns={memberColumns}
                  data={detailQuery.data?.members ?? []}
                />
              </section>
            </div>
          ) : null}
        </SheetContent>
      </Sheet>
      <ConfirmDialog
        open={confirmSuspend}
        onOpenChange={setConfirmSuspend}
        title={t('Suspend organization?')}
        desc={t(
          'Members of a suspended organization cannot use organization management until it is resumed.'
        )}
        confirmText={t('Suspend')}
        destructive
        isLoading={statusMutation.isPending}
        handleConfirm={() => statusMutation.mutate('suspended')}
      />
    </>
  )
}
