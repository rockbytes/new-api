import { useTranslation } from 'react-i18next'

import { formatLogQuota, formatTimestampToDate } from '@/lib/format'

import type { UsageTrendPoint } from '../types'

// Bars share one scale, so a day's width is its share of the largest day.
export function UsageTrendBars(props: {
  points: UsageTrendPoint[]
  locale: string | undefined
}) {
  const { t } = useTranslation()
  const max = Math.max(1, ...props.points.map((point) => point.consume_quota))
  if (props.points.length === 0) {
    return (
      <p className='text-muted-foreground text-sm'>
        {t('No usage in this range')}
      </p>
    )
  }
  return (
    <ul className='flex flex-col gap-2' aria-label={t('Daily net consumption')}>
      {props.points.map((point) => (
        <li
          key={point.day_start}
          className='grid grid-cols-[6rem_1fr_6rem] items-center gap-3 text-sm'
        >
          <span className='text-muted-foreground tabular-nums'>
            {formatTimestampToDate(point.day_start)}
          </span>
          <span className='bg-muted h-3 overflow-hidden rounded'>
            <span
              className='bg-primary block h-full rounded'
              style={{ width: `${(point.consume_quota / max) * 100}%` }}
            />
          </span>
          <span className='text-right tabular-nums'>
            {formatLogQuota(point.consume_quota)}
          </span>
        </li>
      ))}
    </ul>
  )
}
