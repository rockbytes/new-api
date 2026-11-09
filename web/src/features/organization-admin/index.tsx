import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { KeyRound, Plus, Trash2, UserCog, Power } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  DataTableRowActionMenu,
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { EmptyState } from '@/components/empty-state'
import { SectionPageLayout } from '@/components/layout'
import {
  QuotaAdjustDialog,
  type QuotaAdjustMode,
} from '@/components/quota-adjust-dialog'
import { StatusBadge, statusPresets } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenuItem,
  DropdownMenuShortcut,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatTimestamp } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import {
  deleteOrganizationMember,
  getOrganizationMembers,
  getOwnOrganizationQuota,
  setOwnMemberAllocation,
  updateOrganizationMember,
} from './api'
import {
  CreateMemberSheet,
  ResetMemberPasswordSheet,
} from './components/member-sheets'
import { useOrganizationSelf } from './hooks/use-organization-self'
import type { OrganizationMemberRow } from './types'

export function OrganizationAdmin() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [createOpen, setCreateOpen] = useState(false)
  const [resetTarget, setResetTarget] = useState<OrganizationMemberRow | null>(
    null
  )
  const [deleteTarget, setDeleteTarget] =
    useState<OrganizationMemberRow | null>(null)
  const [memberQuery, setMemberQuery] = useState('')

  const selfQuery = useOrganizationSelf()
  const isAdmin = selfQuery.data?.role === 'admin'
  const currentUserId = useAuthStore((state) => state.auth.user?.id)

  const membersQuery = useQuery({
    queryKey: ['organization-members'],
    queryFn: getOrganizationMembers,
    enabled: isAdmin,
  })

  const refreshMembers = () =>
    queryClient.invalidateQueries({ queryKey: ['organization-members'] })

  const changeMutation = useMutation({
    mutationFn: (args: {
      member: OrganizationMemberRow
      changes: { role?: 'admin' | 'member'; status?: 'active' | 'disabled' }
    }) => updateOrganizationMember(args.member.user_id, args.changes),
    onSuccess: () => {
      toast.success(t('Member updated.'))
      refreshMembers()
    },
    onError: (error) => handleServerError(error, t('Failed to update member.')),
  })

  const quotaQuery = useQuery({
    queryKey: ['organization-self-quota'],
    queryFn: getOwnOrganizationQuota,
    enabled: isAdmin,
  })

  const [allocationTarget, setAllocationTarget] =
    useState<OrganizationMemberRow | null>(null)

  const allocationMutation = useMutation({
    mutationFn: (args: { userId: number; allocated: number }) =>
      setOwnMemberAllocation(args.userId, args.allocated),
    onSuccess: () => {
      toast.success(t('Allocation updated.'))
      setAllocationTarget(null)
      refreshMembers()
      queryClient.invalidateQueries({ queryKey: ['organization-self-quota'] })
    },
    onError: (error) =>
      handleServerError(error, t('Failed to update allocation.')),
  })

  // Adds to, subtracts from, or overrides the member's allocation. The allocation cannot fall
  // below the usage the member has already recorded, which the dialog enforces as the minimum.
  const handleAllocate = async (
    mode: QuotaAdjustMode,
    value: number
  ): Promise<boolean> => {
    if (!allocationTarget) return false
    const current = allocationTarget.allocated_quota
    let allocated = value
    if (mode === 'add') allocated = current + value
    if (mode === 'subtract') allocated = current - value
    try {
      await allocationMutation.mutateAsync({
        userId: allocationTarget.user_id,
        allocated,
      })
      return true
    } catch {
      return false
    }
  }

  const deleteMutation = useMutation({
    mutationFn: (member: OrganizationMemberRow) =>
      deleteOrganizationMember(member.user_id),
    onSuccess: () => {
      toast.success(t('Member account deleted.'))
      setDeleteTarget(null)
      refreshMembers()
    },
    onError: (error) => handleServerError(error, t('Failed to delete member.')),
  })

  if (selfQuery.isLoading) return null

  if (selfQuery.isError) {
    const notMember =
      isAxiosError(selfQuery.error) && selfQuery.error.response?.status === 404
    return (
      <EmptyState
        title={
          notMember
            ? t('You are not in an organization')
            : t('Organization management is unavailable')
        }
        description={
          notMember
            ? t(
                'Ask a platform administrator to create or assign your organization.'
              )
            : t('Try again later.')
        }
      />
    )
  }

  if (!isAdmin) {
    return (
      <EmptyState
        title={t('Organization admins only')}
        description={t(
          'Only the organization admin can manage member accounts.'
        )}
      />
    )
  }

  const keyword = memberQuery.trim().toLowerCase()
  const members = (membersQuery.data ?? []).filter((member) =>
    member.username.toLowerCase().includes(keyword)
  )
  const columns: StaticDataTableColumn<OrganizationMemberRow>[] = [
    { id: 'username', header: t('Username'), cell: (row) => row.username },
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
      header: t('Allocated quota'),
      cell: (row) => formatQuotaWithCurrency(row.allocated_quota),
    },
    {
      id: 'used',
      header: t('Used quota'),
      cell: (row) => formatQuotaWithCurrency(row.used_quota),
    },
    {
      id: 'actions',
      header: '',
      cell: (row) => (
        <DataTableRowActionMenu ariaLabel={t('Member actions')}>
          <DropdownMenuItem
            onClick={() =>
              changeMutation.mutate({
                member: row,
                changes: { role: row.role === 'admin' ? 'member' : 'admin' },
              })
            }
          >
            {row.role === 'admin' ? t('Make member') : t('Make admin')}
            <DropdownMenuShortcut>
              <UserCog size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() =>
              changeMutation.mutate({
                member: row,
                changes: {
                  status: row.status === 'active' ? 'disabled' : 'active',
                },
              })
            }
          >
            {row.status === 'active' ? t('Disable') : t('Enable')}
            <DropdownMenuShortcut>
              <Power size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>
          {(row.role === 'member' || row.user_id === currentUserId) && (
            <DropdownMenuItem onClick={() => setAllocationTarget(row)}>
              {t('Allocate quota')}
            </DropdownMenuItem>
          )}
          <DropdownMenuItem onClick={() => setResetTarget(row)}>
            {t('Reset password')}
            <DropdownMenuShortcut>
              <KeyRound size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>
          <DropdownMenuItem
            className='text-destructive focus:text-destructive'
            onClick={() => setDeleteTarget(row)}
          >
            {t('Delete account')}
            <DropdownMenuShortcut>
              <Trash2 size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>
        </DataTableRowActionMenu>
      ),
    },
  ]

  return (
    <>
      <SectionPageLayout fixedContent>
        <SectionPageLayout.Title>
          {selfQuery.data?.organization.name ?? t('Organization management')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button size='sm' onClick={() => setCreateOpen(true)}>
            <Plus className='h-4 w-4' />
            {t('Create member account')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='mb-3 flex flex-wrap items-center justify-between gap-3'>
            <p className='text-muted-foreground text-sm'>
              {t('Remaining quota')}:{' '}
              {formatQuotaWithCurrency(quotaQuery.data?.remaining_quota)}
            </p>
            <Input
              type='search'
              aria-label={t('Search by username')}
              placeholder={t('Search by username')}
              value={memberQuery}
              onChange={(event) => setMemberQuery(event.target.value)}
              className='w-full sm:w-56'
            />
          </div>
          <StaticDataTable columns={columns} data={members} />
        </SectionPageLayout.Content>
      </SectionPageLayout>
      <QuotaAdjustDialog
        open={allocationTarget !== null}
        onOpenChange={(open) => !open && setAllocationTarget(null)}
        currentQuota={allocationTarget?.allocated_quota ?? 0}
        minimumQuota={allocationTarget?.used_quota ?? 0}
        onConfirm={handleAllocate}
      />
      <CreateMemberSheet open={createOpen} onOpenChange={setCreateOpen} />
      <ResetMemberPasswordSheet
        open={resetTarget !== null}
        member={resetTarget}
        onOpenChange={(open) => !open && setResetTarget(null)}
      />
      <ConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t('Delete member account?')}
        desc={t(
          'The account {{username}} and its membership will be permanently deleted. Its past usage stays attributed to this organization.',
          { username: deleteTarget?.username ?? '' }
        )}
        confirmText={t('Delete')}
        destructive
        isLoading={deleteMutation.isPending}
        handleConfirm={() =>
          deleteTarget && deleteMutation.mutate(deleteTarget)
        }
      />
    </>
  )
}
