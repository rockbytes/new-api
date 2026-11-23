import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import { SettingsSection } from '../components/settings-section'
import {
  getRateLimitOverview,
  type RateLimitOverviewLayer,
  type RateLimitSubjectType,
} from './rate-limit-overview-api'

// Read-only view of the effective request limits of one organization, user or channel.
export function RateLimitOverviewSection() {
  const { t } = useTranslation()
  const [subjectType, setSubjectType] =
    useState<RateLimitSubjectType>('organization')
  const [subjectInput, setSubjectInput] = useState('')
  const subjectId = Number.parseInt(subjectInput, 10)
  const validSubject = Number.isInteger(subjectId) && subjectId > 0

  const overview = useQuery({
    queryKey: ['rate-limit-overview', subjectType, subjectId],
    queryFn: () => getRateLimitOverview(subjectType, subjectId),
    enabled: validSubject,
  })

  const subjectLabels: Record<RateLimitSubjectType, string> = {
    organization: t('Organization'),
    user: t('User'),
    channel: t('Channel'),
  }
  const layerLabels: Record<RateLimitOverviewLayer['layer'], string> = {
    organization: t('Organization total requests'),
    user_total: t('User total requests'),
    user_success: t('User successful requests'),
    channel: t('Channel request budget'),
  }

  const columns: StaticDataTableColumn<RateLimitOverviewLayer>[] = [
    {
      id: 'layer',
      header: t('Layer'),
      cell: (row) => layerLabels[row.layer],
    },
    {
      id: 'status',
      header: t('Status'),
      cell: (row) => (row.enabled ? t('Enabled') : t('Disabled')),
    },
    {
      id: 'limit',
      header: t('Limit'),
      cell: (row) => (row.enabled ? row.limit : '-'),
    },
    {
      id: 'window',
      header: t('Window (seconds)'),
      cell: (row) => (row.enabled ? row.window_seconds : '-'),
    },
  ]

  return (
    <SettingsSection title={t('Rate limit overview')}>
      <p className='text-muted-foreground text-sm'>
        {t('Read only. Change the values on their own pages.')}
      </p>
      <div className='flex flex-wrap gap-2'>
        <Select
          value={subjectType}
          onValueChange={(value) =>
            setSubjectType(value as RateLimitSubjectType)
          }
        >
          <SelectTrigger aria-label={t('Subject type')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='organization'>
              {subjectLabels.organization}
            </SelectItem>
            <SelectItem value='user'>{subjectLabels.user}</SelectItem>
            <SelectItem value='channel'>{subjectLabels.channel}</SelectItem>
          </SelectContent>
        </Select>
        <Input
          type='number'
          inputMode='numeric'
          aria-label={t('Subject ID')}
          placeholder={t('Subject ID')}
          value={subjectInput}
          onChange={(event) => setSubjectInput(event.target.value)}
          className='w-40'
        />
      </div>
      {validSubject ? (
        <>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Layers are listed in the order a request meets them. A request is refused by the first enabled layer with no requests left in its window.'
            )}
          </p>
          <StaticDataTable
            columns={columns}
            data={overview.data?.layers ?? []}
          />
        </>
      ) : (
        <p className='text-muted-foreground text-sm'>
          {t('Enter a subject ID to see its limits.')}
        </p>
      )}
      {overview.isError ? (
        <p className='text-destructive text-sm'>
          {t('Failed to load the rate limit overview.')}
        </p>
      ) : null}
    </SettingsSection>
  )
}
