import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import {
  sideDrawerContentClassName,
  sideDrawerHeaderClassName,
  sideDrawerSectionClassName,
} from '@/components/drawer-layout'
import {
  QuotaAdjustDialog,
  type QuotaAdjustMode,
} from '@/components/quota-adjust-dialog'
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
import { CreateMemberSheet } from '@/features/organization-admin/components/member-sheets'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatTimestamp } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import {
  createOrganizationMemberForAdmin,
  getOrganizationDetail,
  setOrganizationMemberAllocation,
  updateOrganizationName,
} from '../api'
import type { OrganizationMember } from '../types'

type OrganizationDetailSheetProps = {
  organizationId: number | null
  onOpenChange: (open: boolean) => void
}

// Renaming and member allocations live here. Status, quota, and deletion are in the list row.
export function OrganizationDetailSheet(props: OrganizationDetailSheetProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const organizationId = props.organizationId
  const [name, setName] = useState('')
  const [allocationTarget, setAllocationTarget] =
    useState<OrganizationMember | null>(null)
  const [memberQuery, setMemberQuery] = useState('')
  const [addMemberOpen, setAddMemberOpen] = useState(false)

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

  // Changes a member's allocation the way the organization balance is adjusted: add,
  // subtract, or override. The member's usage is the lowest value the dialog allows.
  const handleAllocate = async (mode: QuotaAdjustMode, value: number) => {
    if (!allocationTarget) return false
    const current = allocationTarget.allocated_quota
    let allocated = value
    if (mode === 'add') allocated = current + value
    if (mode === 'subtract') allocated = current - value
    try {
      await setOrganizationMemberAllocation(
        organizationId ?? 0,
        allocationTarget.user_id,
        allocated
      )
      toast.success(t('Allocation updated.'))
      setAllocationTarget(null)
      refresh()
      queryClient.invalidateQueries({
        queryKey: ['organization-quota', organizationId],
      })
      return true
    } catch (error) {
      handleServerError(error, t('Failed to update allocation.'))
      return false
    }
  }

  const query = memberQuery.trim().toLowerCase()
  const visibleMembers = (detailQuery.data?.members ?? []).filter((member) =>
    member.username.toLowerCase().includes(query)
  )

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
          <StatusBadge {...statusPresets.active} label={t('Active')} />
        ) : (
          <StatusBadge {...statusPresets.inactive} label={t('Inactive')} />
        ),
    },
    {
      id: 'created',
      header: t('Created'),
      cell: (row) => formatTimestamp(row.created_time),
    },
    {
      id: 'allocated',
      header: t('Total quota'),
      cell: (row) => formatQuotaWithCurrency(row.allocated_quota),
    },
    {
      id: 'remaining',
      header: t('Remaining quota'),
      // Allocation cannot go below usage, so the remainder is never negative.
      cell: (row) =>
        formatQuotaWithCurrency(row.allocated_quota - row.used_quota),
    },
    {
      id: 'actions',
      header: '',
      cell: (row) => (
        <Button
          variant='ghost'
          size='sm'
          className='text-primary hover:text-primary'
          onClick={() => setAllocationTarget(row)}
        >
          {t('Allocate quota')}
        </Button>
      ),
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
              {t('Rename the organization and review its members.')}
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
                <div className='flex flex-wrap items-center justify-between gap-2'>
                  <h3 className='text-sm font-medium'>{t('Members')}</h3>
                  <div className='flex flex-wrap items-center gap-2'>
                    <Input
                      type='search'
                      aria-label={t('Search by username')}
                      placeholder={t('Search by username')}
                      value={memberQuery}
                      onChange={(event) => setMemberQuery(event.target.value)}
                      className='w-48'
                    />
                    <Button size='sm' onClick={() => setAddMemberOpen(true)}>
                      {t('Add member')}
                    </Button>
                  </div>
                </div>
                <StaticDataTable
                  columns={memberColumns}
                  data={visibleMembers}
                />
              </section>
            </div>
          ) : null}
        </SheetContent>
      </Sheet>
      <CreateMemberSheet
        open={addMemberOpen}
        onOpenChange={setAddMemberOpen}
        withGroupOption
        groupScope='all'
        withRoleOption
        submit={(values) =>
          createOrganizationMemberForAdmin(organizationId ?? 0, values)
        }
        onCreated={refresh}
      />
      <QuotaAdjustDialog
        open={allocationTarget !== null}
        onOpenChange={(open) => !open && setAllocationTarget(null)}
        currentQuota={allocationTarget?.allocated_quota ?? 0}
        minimumQuota={allocationTarget?.used_quota ?? 0}
        onConfirm={handleAllocate}
      />
    </>
  )
}
