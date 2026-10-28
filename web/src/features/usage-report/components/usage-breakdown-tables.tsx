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

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatNumber } from '@/lib/format'

import type { UsageModelRow, UsageRankRow } from '../types'

export function UsageModelsTable(props: {
  rows: UsageModelRow[]
  locale: string | undefined
}) {
  const { t } = useTranslation()
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('Model')}</TableHead>
          <TableHead className='text-right'>{t('Net consumption')}</TableHead>
          <TableHead className='text-right'>{t('Requests')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {props.rows.length === 0 && (
          <TableRow>
            <TableCell
              colSpan={3}
              className='text-muted-foreground text-center'
            >
              {t('No usage in this range')}
            </TableCell>
          </TableRow>
        )}
        {props.rows.map((row) => (
          <TableRow key={row.model_name}>
            <TableCell className='font-mono text-xs'>
              {row.model_name}
            </TableCell>
            <TableCell className='text-right tabular-nums'>
              {formatNumber(row.net_quota, props.locale)}
            </TableCell>
            <TableCell className='text-right tabular-nums'>
              {formatNumber(row.requests, props.locale)}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

// Personal billing has key 0 in the organization ranking; it is labeled, not hidden.
export function UsageRankingTable(props: {
  rows: UsageRankRow[]
  locale: string | undefined
  nameFor: (row: UsageRankRow) => string
}) {
  const { t } = useTranslation()
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('Name')}</TableHead>
          <TableHead className='text-right'>{t('Net consumption')}</TableHead>
          <TableHead className='text-right'>{t('Requests')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {props.rows.length === 0 && (
          <TableRow>
            <TableCell
              colSpan={3}
              className='text-muted-foreground text-center'
            >
              {t('No usage in this range')}
            </TableCell>
          </TableRow>
        )}
        {props.rows.map((row) => (
          <TableRow key={row.key}>
            <TableCell>{props.nameFor(row)}</TableCell>
            <TableCell className='text-right tabular-nums'>
              {formatNumber(row.net_quota, props.locale)}
            </TableCell>
            <TableCell className='text-right tabular-nums'>
              {formatNumber(row.requests, props.locale)}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
