import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Combobox } from '@/components/ui/combobox'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { getOrganizationMembers } from '@/features/organization-admin/api'
import { useOrganizationSelf } from '@/features/organization-admin/hooks/use-organization-self'
import {
  getOrganizationDetail,
  getOrganizations,
} from '@/features/organizations/api'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import { toIntlLocale } from '@/i18n/languages'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import {
  getUsageModelOptions,
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
import {
  UsageExportAction,
  UsageExportHistory,
  type UsageExportScopeLabels,
} from './components/usage-export-panel'
import { UsageRecordsTable } from './components/usage-records-table'
import { UsageSummaryCards } from './components/usage-summary-cards'
import { UsageTrendBars } from './components/usage-trend-bars'
import {
  USAGE_PERSONAL_BILLING_KEY,
  USAGE_RANK_BY_LABEL_KEYS,
} from './constants'
import {
  USAGE_RANGE_DAYS,
  buildCustomUsageWindow,
  buildUsageWindow,
  localTzOffsetMinutes,
  type UsageRangeDays,
} from './lib/window'
import type {
  UsagePayerType,
  UsageQueryParams,
  UsageRankBy,
  UsageRankRow,
  UsageView,
} from './types'

const PAGE_SIZE = 10
const ORGANIZATION_OPTION_LIMIT = 100

interface MemberOption {
  id: number
  name: string
}

export function UsageReport() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [payerType, setPayerType] = useState<UsagePayerType>('personal')
  const user = useAuthStore((state) => state.auth.user)
  const isPlatform = (user?.role ?? 0) >= ROLE.ADMIN
  const selfQuery = useOrganizationSelf(!isPlatform)
  const isOrganizationAdmin =
    selfQuery.data?.role === 'admin' && selfQuery.data.status === 'active'
  const scopeReady = Boolean(user) && (isPlatform || selfQuery.isSuccess)
  const [rangeDays, setRangeDays] = useState<UsageRangeDays>(7)
  // A start and end picked by the caller. When set, it replaces the day range.
  const [customRange, setCustomRange] = useState<{
    start: Date
    end: Date
  } | null>(null)
  const [organizationId, setOrganizationId] = useState(0)
  const [memberId, setMemberId] = useState(0)
  const [modelName, setModelName] = useState('')
  const [page, setPage] = useState(1)

  // The window is fixed per range choice, so every query of one view shares the same bounds.
  const window = useMemo(() => {
    const tzOffsetMinutes = localTzOffsetMinutes(new Date())
    if (customRange) {
      return buildCustomUsageWindow(
        customRange.start,
        customRange.end,
        tzOffsetMinutes
      )
    }
    return buildUsageWindow(
      rangeDays,
      Math.floor(Date.now() / 1000),
      tzOffsetMinutes
    )
  }, [rangeDays, customRange])
  // The scope and window shared by every query. The model choices are listed for this scope
  // without the model filter, so they do not shrink when one model is selected.
  let requestedView: UsageView | undefined
  if (isPlatform) requestedView = 'platform'
  else if (payerType === 'personal') requestedView = 'self'
  const scopeParams: UsageQueryParams = {
    ...window,
    view: requestedView,
    payer_type: payerType,
    organization_id:
      payerType === 'organization' && isPlatform
        ? organizationId || undefined
        : undefined,
    user_id: payerType === 'organization' ? memberId || undefined : undefined,
  }
  const params: UsageQueryParams = {
    ...scopeParams,
    model_name: modelName || undefined,
  }
  const summaryQuery = useQuery({
    queryKey: ['usage-report', user?.id, 'summary', params],
    enabled: scopeReady,
    queryFn: () => getUsageSummary(params),
  })
  const trendQuery = useQuery({
    queryKey: ['usage-report', user?.id, 'trend', params],
    enabled: scopeReady,
    queryFn: () => getUsageTrend(params),
  })
  const modelsQuery = useQuery({
    queryKey: ['usage-report', user?.id, 'models', params],
    enabled: scopeReady,
    queryFn: () => getUsageModels(params),
  })
  const rankingsQuery = useQuery({
    queryKey: ['usage-report', user?.id, 'rankings', params],
    enabled: scopeReady,
    queryFn: () => getUsageRankings(params),
  })
  const modelOptionsQuery = useQuery({
    queryKey: ['usage-report', user?.id, 'model-options', scopeParams],
    enabled: scopeReady,
    queryFn: () => getUsageModelOptions(scopeParams),
  })
  const recordsParams = { ...params, page, page_size: PAGE_SIZE }
  const recordsQuery = useQuery({
    queryKey: ['usage-report', user?.id, 'records', recordsParams],
    enabled: scopeReady,
    queryFn: () => getUsageRecords(recordsParams),
  })

  // The server decides the view. Platform administrators pick an organization, and
  // organization administrators pick one of their members.
  const organizationsQuery = useQuery({
    queryKey: ['usage-report', user?.id, 'organizations'],
    queryFn: () => getOrganizations(1, ORGANIZATION_OPTION_LIMIT),
    enabled: payerType === 'organization' && isPlatform,
  })
  const platformMembersQuery = useQuery({
    queryKey: [
      'usage-report',
      user?.id,
      'organization-members',
      organizationId,
    ],
    queryFn: () => getOrganizationDetail(organizationId),
    enabled: payerType === 'organization' && isPlatform && organizationId > 0,
  })
  const ownMembersQuery = useQuery({
    queryKey: ['usage-report', user?.id, 'own-members'],
    queryFn: getOrganizationMembers,
    enabled: payerType === 'organization' && !isPlatform && isOrganizationAdmin,
  })

  const memberOptions: MemberOption[] = isPlatform
    ? (platformMembersQuery.data?.members ?? []).map((member) => ({
        id: member.user_id,
        name: member.username,
      }))
    : (ownMembersQuery.data ?? []).map((member) => ({
        id: member.user_id,
        name: member.username,
      }))

  // The scope as the confirmation names it. An organization admin's report is already scoped to
  // their organization, so the name comes from the response rather than the selector.
  const scopeOrganizationName = summaryQuery.data?.meta.scope.organization?.name
  let exportOrganization = t('All organizations')
  if (payerType === 'personal') {
    exportOrganization = t('Personal')
  } else if (!isPlatform && !isOrganizationAdmin) {
    exportOrganization = t('All organizations')
  } else if (organizationId > 0 || isOrganizationAdmin) {
    exportOrganization = scopeOrganizationName ?? String(organizationId)
  }
  const selectedMemberName = memberOptions.find(
    (member) => member.id === memberId
  )?.name
  let exportMember = t('All members')
  if (!isPlatform && (payerType === 'personal' || !isOrganizationAdmin)) {
    exportMember = t('Only Mine')
  } else if (memberId > 0) {
    exportMember = selectedMemberName ?? String(memberId)
  }
  const exportScope: UsageExportScopeLabels = {
    payer: payerType === 'personal' ? t('Personal') : t('Organization'),
    organization: exportOrganization,
    member: exportMember,
    model: modelName || t('All models'),
  }

  const rankBy: UsageRankBy | '' = rankingsQuery.data?.rank_by ?? ''
  const rankingName = (row: UsageRankRow): string => {
    if (rankBy === 'organization' && row.key === 0) {
      return t(USAGE_PERSONAL_BILLING_KEY)
    }
    return row.name || String(row.key)
  }

  const rangePresets = USAGE_RANGE_DAYS.map((days) => ({
    value: days,
    label: t('Last {{count}} days', { count: days }),
  }))

  const failed =
    summaryQuery.isError ||
    trendQuery.isError ||
    modelsQuery.isError ||
    rankingsQuery.isError ||
    recordsQuery.isError

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Usage report')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-4'>
          <Tabs
            value={payerType}
            onValueChange={(value) => {
              if (value !== 'personal' && value !== 'organization') return
              setPayerType(value)
              setOrganizationId(0)
              setMemberId(0)
              setModelName('')
              setPage(1)
            }}
          >
            <TabsList aria-label={t('Paid by')}>
              <TabsTrigger value='personal'>{t('Personal')}</TabsTrigger>
              <TabsTrigger value='organization'>
                {t('Organization')}
              </TabsTrigger>
            </TabsList>
          </Tabs>
          <Card size='sm'>
            <CardContent className='flex flex-wrap items-center gap-3'>
              {payerType === 'organization' && isPlatform && (
                <>
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
                      setModelName('')
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
                        setModelName('')
                        setPage(1)
                      }}
                    />
                  )}
                </>
              )}
              {payerType === 'organization' &&
                !isPlatform &&
                isOrganizationAdmin && (
                  <MemberSelect
                    value={memberId}
                    options={memberOptions}
                    onChange={(id) => {
                      setMemberId(id)
                      setModelName('')
                      setPage(1)
                    }}
                  />
                )}
              <Combobox
                options={[
                  { value: '', label: t('All models') },
                  ...(modelOptionsQuery.data?.models ?? []).map((name) => ({
                    value: name,
                    label: name,
                  })),
                ]}
                value={modelName}
                onValueChange={(value) => {
                  setModelName(value ?? '')
                  setPage(1)
                }}
                placeholder={t('Model')}
                searchPlaceholder={t('Search models')}
                emptyText={t('No matching results')}
                className='w-full sm:w-64'
              />
              <div
                className='flex w-full max-w-full min-w-0 flex-wrap items-center gap-2 sm:ml-auto sm:w-auto'
                role='group'
                aria-label={t('Time range')}
              >
                <Select
                  items={rangePresets}
                  value={customRange ? null : rangeDays}
                  onValueChange={(days) => {
                    if (days == null) return
                    setRangeDays(days)
                    setCustomRange(null)
                    setPage(1)
                  }}
                >
                  <SelectTrigger aria-label={t('Time range')}>
                    <SelectValue placeholder={t('Custom')} />
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    {rangePresets.map((preset) => (
                      <SelectItem key={preset.value} value={preset.value}>
                        {preset.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <div className='w-full min-w-0 sm:w-88 sm:max-w-full'>
                  <CompactDateTimeRangePicker
                    start={new Date(window.start_timestamp * 1000)}
                    end={new Date(window.end_timestamp * 1000)}
                    onChange={({ start, end }) => {
                      if (!start && !end) {
                        setCustomRange(null)
                        setPage(1)
                        return
                      }
                      if (start && end && end.getTime() > start.getTime()) {
                        setCustomRange({ start, end })
                        setPage(1)
                      }
                    }}
                  />
                </div>
                {scopeReady && (
                  <div className='ml-auto sm:ml-1'>
                    <UsageExportAction
                      key={`${user?.id}-${payerType}`}
                      params={params}
                      scope={exportScope}
                    />
                  </div>
                )}
              </div>
            </CardContent>
          </Card>
          {!isPlatform && selfQuery.isError && (
            <div
              role='alert'
              className='text-destructive flex items-center gap-2 text-sm'
            >
              {t('The usage report could not be loaded')}
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => selfQuery.refetch()}
              >
                {t('Retry')}
              </Button>
            </div>
          )}
          {failed && (
            <p role='alert' className='text-destructive text-sm'>
              {t('The usage report could not be loaded')}
            </p>
          )}
          {summaryQuery.data && (
            <UsageSummaryCards
              summary={summaryQuery.data.summary}
              locale={locale}
            />
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
              <CardTitle>{t('Export history')}</CardTitle>
            </CardHeader>
            <CardContent>
              {scopeReady && <UsageExportHistory key={user?.id} />}
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
