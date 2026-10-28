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

import { cn } from '@/lib/utils'

import { USAGE_VIEW_LABEL_KEYS } from '../constants'
import type { UsageMeta } from '../types'

// The scope tells the reader what the figures cover, in the caller's own time zone.
export function UsageScopeHeader(props: {
  meta: UsageMeta
  locale: string | undefined
  startLabel: string
  endLabel: string
}) {
  const { t } = useTranslation()
  const organization = props.meta.scope.organization
  return (
    <div className='bg-muted/40 flex flex-col gap-1 rounded-lg border p-3 text-sm'>
      <div className='flex flex-wrap items-center gap-2'>
        <span className='font-medium'>
          {t(USAGE_VIEW_LABEL_KEYS[props.meta.scope.view])}
        </span>
        {organization && (
          <span className='text-muted-foreground'>· {organization.name}</span>
        )}
      </div>
      <p className='text-muted-foreground'>
        {t('Time range')}: {props.startLabel} – {props.endLabel}
        {' · '}
        {t('Time zone offset')}:{' '}
        {formatOffset(props.meta.window.tz_offset_minutes)}
      </p>
      <p className={cn('text-muted-foreground')}>
        {t('Settled net consumption, in platform quota units, not currency')}
      </p>
    </div>
  )
}

function formatOffset(minutes: number): string {
  const sign = minutes < 0 ? '-' : '+'
  const absolute = Math.abs(minutes)
  const hours = String(Math.floor(absolute / 60)).padStart(2, '0')
  const mins = String(absolute % 60).padStart(2, '0')
  return `UTC${sign}${hours}:${mins}`
}
