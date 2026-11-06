import { useTranslation } from 'react-i18next'

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatLogQuota, formatNumber } from '@/lib/format'

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
          <TableHead className='text-right'>{t('Consumed')}</TableHead>
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
              {formatLogQuota(row.consume_quota)}
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
          <TableHead className='text-right'>{t('Consumed')}</TableHead>
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
              {formatLogQuota(row.consume_quota)}
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
