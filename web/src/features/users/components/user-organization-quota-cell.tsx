import { useTranslation } from 'react-i18next'

import { QuotaDetailsPopover } from '@/components/quota-details-popover'
import { formatQuotaWithCurrency, getCurrencyDisplay } from '@/lib/currency'
import { cn } from '@/lib/utils'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { QuotaRow } from './quota-row'

type UserOrganizationQuotaCellProps = {
  allocated: number
  used: number
}

/**
 * A member's organization allocation: how much the organization lets this member spend
 * through company-paid Keys. It is separate from the member's personal balance.
 */
export function UserOrganizationQuotaCell(
  props: UserOrganizationQuotaCellProps
) {
  const { t } = useTranslation()
  useSystemConfigStore((state) => state.config.currency)

  const { meta: currency } = getCurrencyDisplay()
  const quotaUnit = currency.kind === 'tokens' ? t('Tokens') : currency.symbol
  const remaining = props.allocated - props.used
  const formattedRemaining = formatQuotaWithCurrency(remaining, {
    showSymbol: false,
  })
  const formattedAllocated = formatQuotaWithCurrency(props.allocated, {
    showSymbol: false,
  })
  const formattedUsed = formatQuotaWithCurrency(props.used, {
    showSymbol: false,
  })

  return (
    <QuotaDetailsPopover
      title={`${t('Organization allocation')} (${quotaUnit})`}
      triggerLabel={`${t('Organization allocation')} ${t('Remaining')} ${formattedRemaining}`}
      details={[
        { label: t('Allocated quota'), value: formattedAllocated },
        { label: t('Total Used'), value: formattedUsed },
      ]}
    >
      <QuotaRow
        label={t('Organization')}
        value={formattedRemaining}
        valueClassName={cn(
          remaining < 0 && 'text-destructive',
          remaining === 0 && 'text-muted-foreground'
        )}
        secondaryLabel={t('Allocated quota')}
        secondaryValue={formattedAllocated}
      />
    </QuotaDetailsPopover>
  )
}
