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
import { useQuery } from '@tanstack/react-query'
import { Eye, Plus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { SectionPageLayout } from '@/components/layout'
import { StatusBadge, statusPresets } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { formatTimestamp } from '@/lib/format'

import { getOrganizations } from './api'
import { CreateOrganizationSheet } from './components/create-organization-sheet'
import { OrganizationDetailSheet } from './components/organization-detail-sheet'
import type { Organization } from './types'

const PAGE_SIZE = 100

export function Organizations() {
  const { t } = useTranslation()
  const [createOpen, setCreateOpen] = useState(false)
  const [selectedId, setSelectedId] = useState<number | null>(null)

  const listQuery = useQuery({
    queryKey: ['organizations', PAGE_SIZE],
    queryFn: () => getOrganizations(1, PAGE_SIZE),
  })

  const columns: StaticDataTableColumn<Organization>[] = [
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
          <StatusBadge {...statusPresets.active} />
        ) : (
          <StatusBadge {...statusPresets.suspended} />
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
        <Button
          variant='ghost'
          size='icon-sm'
          aria-label={t('View organization')}
          onClick={() => setSelectedId(row.id)}
        >
          <Eye />
        </Button>
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
    </>
  )
}
