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
import { useTranslation } from 'react-i18next'

import { formatNumber, formatTimestampToDate } from '@/lib/format'

import type { UsageTrendPoint } from '../types'

// Bars share one scale, so a day's width is its share of the largest day.
export function UsageTrendBars(props: {
  points: UsageTrendPoint[]
  locale: string | undefined
}) {
  const { t } = useTranslation()
  const max = Math.max(1, ...props.points.map((point) => point.net_quota))
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
              style={{ width: `${(point.net_quota / max) * 100}%` }}
            />
          </span>
          <span className='text-right tabular-nums'>
            {formatNumber(point.net_quota, props.locale)}
          </span>
        </li>
      ))}
    </ul>
  )
}
