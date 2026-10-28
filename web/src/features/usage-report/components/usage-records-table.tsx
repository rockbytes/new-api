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

import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatNumber, formatTimestamp } from '@/lib/format'

import type { UsageRecordItem } from '../api'
import type { UsagePlatformRecord, UsageSelfRecord, UsageView } from '../types'

const REFUND_LOG_TYPE = 6

export function UsageRecordsTable(props: {
  view: UsageView
  items: UsageRecordItem[]
  total: number
  page: number
  pageSize: number
  locale: string | undefined
  onPageChange: (page: number) => void
}) {
  const { t } = useTranslation()
  const pageCount = Math.max(1, Math.ceil(props.total / props.pageSize))
  const showPayer = props.view === 'self'
  const showOperations = props.view === 'platform'
  return (
    <div className='flex flex-col gap-3'>
      <div className='overflow-x-auto'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Time')}</TableHead>
              <TableHead>{t('Model')}</TableHead>
              <TableHead>{t('Member')}</TableHead>
              <TableHead className='text-right'>{t('Quota')}</TableHead>
              <TableHead className='text-right'>{t('Prompt tokens')}</TableHead>
              <TableHead className='text-right'>
                {t('Completion tokens')}
              </TableHead>
              {showPayer && <TableHead>{t('Paid by')}</TableHead>}
              {showOperations && <TableHead>{t('Channel')}</TableHead>}
            </TableRow>
          </TableHeader>
          <TableBody>
            {props.items.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={8}
                  className='text-muted-foreground text-center'
                >
                  {t('No usage in this range')}
                </TableCell>
              </TableRow>
            )}
            {props.items.map((item) => (
              <TableRow key={item.id}>
                <TableCell className='whitespace-nowrap'>
                  {formatTimestamp(item.created_at)}
                </TableCell>
                <TableCell className='font-mono text-xs'>
                  {item.model_name}
                </TableCell>
                <TableCell>{item.username}</TableCell>
                <TableCell className='text-right tabular-nums'>
                  {item.type === REFUND_LOG_TYPE ? '-' : ''}
                  {formatNumber(item.quota, props.locale)}
                </TableCell>
                <TableCell className='text-right tabular-nums'>
                  {formatNumber(item.prompt_tokens, props.locale)}
                </TableCell>
                <TableCell className='text-right tabular-nums'>
                  {formatNumber(item.completion_tokens, props.locale)}
                </TableCell>
                {showPayer && (
                  <TableCell>
                    {payerLabel(item as UsageSelfRecord, t)}
                  </TableCell>
                )}
                {showOperations && (
                  <TableCell>
                    {(item as UsagePlatformRecord).channel_id}
                  </TableCell>
                )}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      <div className='flex items-center justify-between gap-3 text-sm'>
        <span className='text-muted-foreground'>
          {t('Page {{page}} of {{count}}', {
            page: props.page,
            count: pageCount,
          })}
        </span>
        <div className='flex gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={props.page <= 1}
            onClick={() => props.onPageChange(props.page - 1)}
          >
            {t('Previous')}
          </Button>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={props.page >= pageCount}
            onClick={() => props.onPageChange(props.page + 1)}
          >
            {t('Next')}
          </Button>
        </div>
      </div>
    </div>
  )
}

function payerLabel(
  record: UsageSelfRecord,
  t: (key: string) => string
): string {
  if (record.billing_organization_id > 0)
    return record.billing_organization_name
  return t('Personal')
}
