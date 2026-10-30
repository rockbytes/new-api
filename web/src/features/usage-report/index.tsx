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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Combobox } from '@/components/ui/combobox'
import { getOrganizationMembers } from '@/features/organization-admin/api'
import {
  getOrganizationDetail,
  getOrganizations,
} from '@/features/organizations/api'
import { toIntlLocale } from '@/i18n/languages'
import { formatTimestamp } from '@/lib/format'

import {
  getUsageModels,
  getUsageRankings,
  getUsageRecords,
  getUsageSummary,
  getUsageTrend,
} from './api'
import {
  UsageModelsTable,
  UsageRankingTable,
} from './components/usage-breakdown-tables'
import { UsageExportPanel } from './components/usage-export-panel'
import { UsageRecordsTable } from './components/usage-records-table'
import { UsageScopeHeader } from './components/usage-scope-header'
import { UsageSummaryCards } from './components/usage-summary-cards'
import { UsageTrendBars } from './components/usage-trend-bars'
import {
  USAGE_PERSONAL_BILLING_KEY,
  USAGE_RANK_BY_LABEL_KEYS,
} from './constants'
import {
  USAGE_RANGE_DAYS,
  buildUsageWindow,
  localTzOffsetMinutes,
  type UsageRangeDays,
} from './lib/window'
import type {
  UsageQueryParams,
  UsageRankBy,
  UsageRankRow,
  UsageView,
} from './types'

const PAGE_SIZE = 20
const ORGANIZATION_OPTION_LIMIT = 100

interface MemberOption {
  id: number
  name: string
}

export function UsageReport() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [rangeDays, setRangeDays] = useState<UsageRangeDays>(7)
  const [organizationId, setOrganizationId] = useState(0)
  const [memberId, setMemberId] = useState(0)
  const [page, setPage] = useState(1)

  // The window is fixed per range choice, so every query of one view shares the same bounds.
  const window = useMemo(
    () =>
      buildUsageWindow(
        rangeDays,
        Math.floor(Date.now() / 1000),
        localTzOffsetMinutes(new Date())
      ),
    [rangeDays]
  )
  const params: UsageQueryParams = {
    ...window,
    view: organizationId > 0 ? 'platform' : undefined,
    organization_id: organizationId || undefined,
    user_id: memberId || undefined,
  }
  const summaryQuery = useQuery({
    queryKey: ['usage-report', 'summary', params],
    queryFn: () => getUsageSummary(params),
  })
  const trendQuery = useQuery({
    queryKey: ['usage-report', 'trend', params],
    queryFn: () => getUsageTrend(params),
  })
  const modelsQuery = useQuery({
    queryKey: ['usage-report', 'models', params],
    queryFn: () => getUsageModels(params),
  })
  const rankingsQuery = useQuery({
    queryKey: ['usage-report', 'rankings', params],
    queryFn: () => getUsageRankings(params),
  })
  const recordsParams = { ...params, page, page_size: PAGE_SIZE }
  const recordsQuery = useQuery({
    queryKey: ['usage-report', 'records', recordsParams],
    queryFn: () => getUsageRecords(recordsParams),
  })

  // The server decides the view. Platform administrators pick an organization, and
  // organization administrators pick one of their members.
  const view: UsageView | undefined = summaryQuery.data?.meta.scope.view
  const organizationsQuery = useQuery({
    queryKey: ['usage-report', 'organizations'],
    queryFn: () => getOrganizations(1, ORGANIZATION_OPTION_LIMIT),
    enabled: view === 'platform',
  })
  const platformMembersQuery = useQuery({
    queryKey: ['usage-report', 'organization-members', organizationId],
    queryFn: () => getOrganizationDetail(organizationId),
    enabled: view === 'platform' && organizationId > 0,
  })
  const ownMembersQuery = useQuery({
    queryKey: ['usage-report', 'own-members'],
    queryFn: getOrganizationMembers,
    enabled: view === 'organization',
  })

  const memberOptions: MemberOption[] =
    view === 'platform'
      ? (platformMembersQuery.data?.members ?? []).map((member) => ({
          id: member.user_id,
          name: member.username,
        }))
      : (ownMembersQuery.data ?? []).map((member) => ({
          id: member.user_id,
          name: member.username,
        }))

  const rankBy: UsageRankBy | '' = rankingsQuery.data?.rank_by ?? ''
  const rankingName = (row: UsageRankRow): string => {
    if (rankBy === 'organization' && row.key === 0) {
      return t(USAGE_PERSONAL_BILLING_KEY)
    }
    return row.name || String(row.key)
  }

  const failed =
    summaryQuery.isError ||
    trendQuery.isError ||
    modelsQuery.isError ||
    rankingsQuery.isError ||
    recordsQuery.isError

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Usage report')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <div
          className='flex flex-wrap gap-2'
          role='group'
          aria-label={t('Time range')}
        >
          {USAGE_RANGE_DAYS.map((days) => (
            <Button
              key={days}
              type='button'
              size='sm'
              variant={rangeDays === days ? 'default' : 'outline'}
              aria-pressed={rangeDays === days}
              onClick={() => {
                setRangeDays(days)
                setPage(1)
              }}
            >
              {t('Last {{count}} days', { count: days })}
            </Button>
          ))}
        </div>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-4'>
          {view === 'platform' && (
            <Card size='sm'>
              <CardContent className='flex flex-wrap gap-3'>
                <Combobox
                  options={[
                    { value: '0', label: t('All organizations') },
                    ...(organizationsQuery.data?.items ?? []).map(
                      (organization) => ({
                        value: String(organization.id),
                        label: organization.name,
                      })
                    ),
                  ]}
                  value={String(organizationId)}
                  onValueChange={(value) => {
                    setOrganizationId(Number(value ?? 0))
                    setMemberId(0)
                    setPage(1)
                  }}
                  placeholder={t('Organization')}
                  searchPlaceholder={t('Search organizations')}
                  emptyText={t('No matching results')}
                  className='w-full sm:w-72'
                />
                {organizationId > 0 && (
                  <MemberSelect
                    value={memberId}
                    options={memberOptions}
                    onChange={(id) => {
                      setMemberId(id)
                      setPage(1)
                    }}
                  />
                )}
              </CardContent>
            </Card>
          )}
          {view === 'organization' && (
            <Card size='sm'>
              <CardContent className='flex flex-wrap gap-3'>
                <MemberSelect
                  value={memberId}
                  options={memberOptions}
                  onChange={(id) => {
                    setMemberId(id)
                    setPage(1)
                  }}
                />
              </CardContent>
            </Card>
          )}
          {failed && (
            <p role='alert' className='text-destructive text-sm'>
              {t('The usage report could not be loaded')}
            </p>
          )}
          {summaryQuery.data && (
            <>
              <UsageScopeHeader
                meta={summaryQuery.data.meta}
                locale={locale}
                startLabel={formatTimestamp(window.start_timestamp)}
                endLabel={formatTimestamp(window.end_timestamp)}
              />
              <UsageSummaryCards
                summary={summaryQuery.data.summary}
                locale={locale}
              />
            </>
          )}
          <Card>
            <CardHeader>
              <CardTitle>{t('Trend')}</CardTitle>
            </CardHeader>
            <CardContent>
              <UsageTrendBars
                points={trendQuery.data?.points ?? []}
                locale={locale}
              />
            </CardContent>
          </Card>
          <div className='grid gap-4 lg:grid-cols-2'>
            <Card>
              <CardHeader>
                <CardTitle>{t('Models')}</CardTitle>
              </CardHeader>
              <CardContent>
                <UsageModelsTable
                  rows={modelsQuery.data?.models ?? []}
                  locale={locale}
                />
              </CardContent>
            </Card>
            {rankBy !== '' && (
              <Card>
                <CardHeader>
                  <CardTitle>{t(USAGE_RANK_BY_LABEL_KEYS[rankBy])}</CardTitle>
                </CardHeader>
                <CardContent>
                  <UsageRankingTable
                    rows={rankingsQuery.data?.rows ?? []}
                    locale={locale}
                    nameFor={rankingName}
                  />
                </CardContent>
              </Card>
            )}
          </div>
          <Card>
            <CardHeader>
              <CardTitle>{t('Records')}</CardTitle>
            </CardHeader>
            <CardContent>
              {recordsQuery.data && (
                <UsageRecordsTable
                  view={recordsQuery.data.meta.scope.view}
                  items={recordsQuery.data.items}
                  total={recordsQuery.data.total}
                  page={page}
                  pageSize={PAGE_SIZE}
                  locale={locale}
                  onPageChange={setPage}
                />
              )}
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>{t('Export usage')}</CardTitle>
            </CardHeader>
            <CardContent>
              <UsageExportPanel params={params} />
            </CardContent>
          </Card>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function MemberSelect(props: {
  value: number
  options: MemberOption[]
  onChange: (id: number) => void
}) {
  const { t } = useTranslation()
  return (
    <Combobox
      options={[
        { value: '0', label: t('All members') },
        ...props.options.map((member) => ({
          value: String(member.id),
          label: member.name,
        })),
      ]}
      value={String(props.value)}
      onValueChange={(value) => props.onChange(Number(value ?? 0))}
      placeholder={t('Member')}
      searchPlaceholder={t('Search members')}
      emptyText={t('No matching results')}
      className='w-full sm:w-64'
    />
  )
}
