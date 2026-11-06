import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { SectionPageLayout } from '@/components/layout'
import {
  QuotaAdjustDialog,
  type QuotaAdjustMode,
} from '@/components/quota-adjust-dialog'
import { StatusBadge, statusPresets } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatTimestamp } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import {
  adjustOrganizationQuota,
  getOrganizations,
  updateOrganizationStatus,
} from './api'
import { CreateOrganizationSheet } from './components/create-organization-sheet'
import { OrganizationDeleteDialog } from './components/organization-delete-dialog'
import { OrganizationDetailSheet } from './components/organization-detail-sheet'
import type { OrganizationStatus, OrganizationSummary } from './types'

const PAGE_SIZE = 100

export function Organizations() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [createOpen, setCreateOpen] = useState(false)
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [adjustTarget, setAdjustTarget] = useState<OrganizationSummary | null>(
    null
  )
  const [suspendTarget, setSuspendTarget] =
    useState<OrganizationSummary | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<OrganizationSummary | null>(
    null
  )

  const listQuery = useQuery({
    queryKey: ['organizations', PAGE_SIZE],
    queryFn: () => getOrganizations(1, PAGE_SIZE),
  })

  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: ['organizations'] })
  }

  const statusMutation = useMutation({
    mutationFn: (change: { id: number; status: OrganizationStatus }) =>
      updateOrganizationStatus(change.id, change.status),
    onSuccess: () => {
      toast.success(t('Organization status updated.'))
      setSuspendTarget(null)
      refresh()
    },
    onError: (error) =>
      handleServerError(error, t('Failed to update organization status.')),
  })

  // The organization balance is the unallocated pool. Adjusting it works the same way as a
  // user's balance: add, subtract, or override.
  const handleAdjust = async (
    mode: QuotaAdjustMode,
    value: number
  ): Promise<boolean> => {
    if (!adjustTarget) return false
    try {
      await adjustOrganizationQuota(adjustTarget.id, {
        mode,
        amount: value,
        reason: '',
        reference: `adjust-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`,
      })
      toast.success(t('Quota adjusted successfully'))
      setAdjustTarget(null)
      refresh()
      return true
    } catch (error) {
      handleServerError(error, t('Failed to adjust quota'))
      return false
    }
  }

  const columns: StaticDataTableColumn<OrganizationSummary>[] = [
    {
      id: 'name',
      header: t('Organization'),
      cell: (row) => row.name,
    },
    {
      id: 'status',
      header: t('Status'),
      cell: (row) =>
        row.status === 'active' ? (
          <StatusBadge {...statusPresets.active} label={t('Active')} />
        ) : (
          <StatusBadge {...statusPresets.suspended} label={t('Suspended')} />
        ),
    },
    {
      id: 'members',
      header: t('Members'),
      cell: (row) => row.member_count,
    },
    {
      id: 'granted',
      header: t('Total quota'),
      cell: (row) => formatQuotaWithCurrency(row.granted_quota),
    },
    {
      id: 'allocated',
      header: t('Allocated quota'),
      cell: (row) => formatQuotaWithCurrency(row.allocated_quota),
    },
    {
      id: 'unallocated',
      header: t('Organization balance'),
      cell: (row) => formatQuotaWithCurrency(row.unallocated_quota),
    },
    {
      id: 'created',
      header: t('Created'),
      cell: (row) => formatTimestamp(row.created_time),
    },
    {
      id: 'actions',
      header: '',
      cell: (row) => (
        <div className='flex flex-wrap items-center justify-end gap-1'>
          <Button
            variant='ghost'
            size='sm'
            onClick={() => setSelectedId(row.id)}
          >
            {t('Edit')}
          </Button>
          <Button
            variant='ghost'
            size='sm'
            onClick={() => setAdjustTarget(row)}
          >
            {t('Adjust Quota')}
          </Button>
          {row.status === 'active' ? (
            <Button
              variant='ghost'
              size='sm'
              onClick={() => setSuspendTarget(row)}
            >
              {t('Suspend')}
            </Button>
          ) : (
            <Button
              variant='ghost'
              size='sm'
              disabled={statusMutation.isPending}
              onClick={() =>
                statusMutation.mutate({ id: row.id, status: 'active' })
              }
            >
              {t('Resume')}
            </Button>
          )}
          <Button
            variant='ghost'
            size='sm'
            className='text-destructive hover:text-destructive'
            onClick={() => setDeleteTarget(row)}
          >
            {t('Delete')}
          </Button>
        </div>
      ),
    },
  ]

  return (
    <>
      <SectionPageLayout fixedContent>
        <SectionPageLayout.Title>{t('Organizations')}</SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button size='sm' onClick={() => setCreateOpen(true)}>
            <Plus className='h-4 w-4' />
            {t('Create organization')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <StaticDataTable
            columns={columns}
            data={listQuery.data?.items ?? []}
          />
        </SectionPageLayout.Content>
      </SectionPageLayout>
      <CreateOrganizationSheet open={createOpen} onOpenChange={setCreateOpen} />
      <OrganizationDetailSheet
        organizationId={selectedId}
        onOpenChange={(open) => !open && setSelectedId(null)}
      />
      <QuotaAdjustDialog
        open={adjustTarget !== null}
        onOpenChange={(open) => !open && setAdjustTarget(null)}
        currentQuota={adjustTarget?.unallocated_quota ?? 0}
        minimumQuota={0}
        onConfirm={handleAdjust}
      />
      <ConfirmDialog
        open={suspendTarget !== null}
        onOpenChange={(open) => !open && setSuspendTarget(null)}
        title={t('Suspend organization?')}
        desc={t(
          'Members of a suspended organization cannot use organization management until it is resumed.'
        )}
        confirmText={t('Suspend')}
        destructive
        isLoading={statusMutation.isPending}
        handleConfirm={() =>
          suspendTarget &&
          statusMutation.mutate({ id: suspendTarget.id, status: 'suspended' })
        }
      />
      <OrganizationDeleteDialog
        organizationId={deleteTarget?.id ?? 0}
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        onDeleted={() => {
          setDeleteTarget(null)
          refresh()
        }}
      />
    </>
  )
}
