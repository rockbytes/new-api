import { useTranslation } from 'react-i18next'

import { QuotaDetailsPopover } from '@/components/quota-details-popover'
import { formatQuotaWithCurrency, getCurrencyDisplay } from '@/lib/currency'
import { cn } from '@/lib/utils'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { QuotaRow } from './quota-row'

type UserQuotaCellProps = {
  remaining: number
  personalKeyUsage?: number | null
}

export function UserQuotaCell(props: UserQuotaCellProps) {
  const { t } = useTranslation()
  useSystemConfigStore((state) => state.config.currency)

  const { meta: currency } = getCurrencyDisplay()
  const quotaUnit = currency.kind === 'tokens' ? t('Tokens') : currency.symbol
  const formattedRemaining = formatQuotaWithCurrency(props.remaining, {
    showSymbol: false,
  })
  const formattedUsed =
    props.personalKeyUsage == null
      ? '—'
      : formatQuotaWithCurrency(props.personalKeyUsage, { showSymbol: false })
  const usageLabel = t('Personal Key usage')

  return (
    <QuotaDetailsPopover
      title={`${t('Quota')} (${quotaUnit})`}
      triggerLabel={`${t('Available Balance')} ${formattedRemaining}; ${usageLabel} ${props.personalKeyUsage == null ? t('Unavailable') : formattedUsed}`}
      details={[
        { label: t('Available Balance'), value: formattedRemaining },
        { label: usageLabel, value: formattedUsed },
      ]}
      description={t(
        'Cumulative usage of personally paid API Keys, including wallet and subscription payments. Refunds are not deducted. Historical usage may be incomplete.'
      )}
    >
      <QuotaRow
        label={t('Available')}
        value={formattedRemaining}
        valueClassName={cn(
          props.remaining < 0 && 'text-destructive',
          props.remaining === 0 && 'text-muted-foreground'
        )}
        secondaryLabel={usageLabel}
        secondaryValue={formattedUsed}
      />
    </QuotaDetailsPopover>
  )
}
