import { useTranslation } from 'react-i18next'

import { QuotaDetailsPopover } from '@/components/quota-details-popover'
import { StatusBadge } from '@/components/status-badge'
import { formatQuotaWithCurrency, getCurrencyDisplay } from '@/lib/currency'
import { cn } from '@/lib/utils'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { QuotaRow } from './quota-row'

type UserQuotaCellProps = {
  remaining: number
  used: number
}

export function UserQuotaCell(props: UserQuotaCellProps) {
  const { t } = useTranslation()
  useSystemConfigStore((state) => state.config.currency)

  const { meta: currency } = getCurrencyDisplay()
  const quotaUnit = currency.kind === 'tokens' ? t('Tokens') : currency.symbol
  const hasQuota = props.remaining !== 0 || props.used !== 0
  const formattedRemaining = formatQuotaWithCurrency(props.remaining, {
    showSymbol: false,
  })
  const formattedUsed = formatQuotaWithCurrency(props.used, {
    showSymbol: false,
  })

  return (
    <QuotaDetailsPopover
      title={`${t('Quota')} (${quotaUnit})`}
      triggerLabel={
        hasQuota
          ? `${t('Available Balance')} ${formattedRemaining}; ${t('Used amount')} ${formattedUsed}`
          : t('No Quota')
      }
      details={[
        { label: t('Available Balance'), value: formattedRemaining },
        { label: t('Total Used'), value: formattedUsed },
      ]}
    >
      {hasQuota ? (
        <QuotaRow
          label={t('Personal')}
          value={formattedRemaining}
          valueClassName={cn(
            props.remaining < 0 && 'text-destructive',
            props.remaining === 0 && 'text-muted-foreground'
          )}
          secondaryLabel={t('Used amount')}
          secondaryValue={formattedUsed}
        />
      ) : (
        <QuotaRow
          label={t('Personal')}
          badge={
            <StatusBadge
              label={t('No Quota')}
              variant='neutral'
              copyable={false}
              className='font-normal'
            />
          }
        />
      )}
    </QuotaDetailsPopover>
  )
}
