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

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatNumber } from '@/lib/format'

import type { UsageSummary } from '../types'

export function UsageSummaryCards(props: {
  summary: UsageSummary
  locale: string | undefined
}) {
  const { t } = useTranslation()
  const cards = [
    { key: 'net', label: t('Net consumption'), value: props.summary.net_quota },
    {
      key: 'consumed',
      label: t('Consumed'),
      value: props.summary.consume_quota,
    },
    {
      key: 'refunded',
      label: t('Refunded'),
      value: props.summary.refund_quota,
    },
    { key: 'requests', label: t('Requests'), value: props.summary.requests },
    {
      key: 'prompt',
      label: t('Prompt tokens'),
      value: props.summary.prompt_tokens,
    },
    {
      key: 'completion',
      label: t('Completion tokens'),
      value: props.summary.completion_tokens,
    },
  ]
  return (
    <div className='grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6'>
      {cards.map((card) => (
        <Card key={card.key} size='sm'>
          <CardHeader>
            <CardTitle className='text-muted-foreground text-sm font-medium'>
              {card.label}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className='text-xl font-semibold tabular-nums'>
              {formatNumber(card.value, props.locale)}
            </p>
          </CardContent>
        </Card>
      ))}
    </div>
  )
}
