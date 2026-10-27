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
import { StatusBadge, statusPresets } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenuItem,
  DropdownMenuShortcut,
} from '@/components/ui/dropdown-menu'
import { formatTimestamp } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import {
  deleteOrganizationMember,
  getOrganizationMembers,
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

  const selfQuery = useOrganizationSelf()
  const isAdmin = selfQuery.data?.role === 'admin'

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

  const members = membersQuery.data ?? []
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
          <StaticDataTable columns={columns} data={members} />
        </SectionPageLayout.Content>
      </SectionPageLayout>
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
