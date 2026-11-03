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
    {
      key: 'consumed',
      label: t('Consumed'),
      value: props.summary.consume_quota,
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
    <div className='grid grid-cols-2 gap-3 md:grid-cols-4'>
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
