import { useTranslation } from 'react-i18next'

import { QuotaDetailsPopover } from '@/components/quota-details-popover'
import { formatQuotaWithCurrency, getCurrencyDisplay } from '@/lib/currency'
import { cn } from '@/lib/utils'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { QuotaRow } from './quota-row'

type UserOrganizationQuotaCellProps = {
  allocated: number
  used: number
  // What the organization itself can still spend. A member is bounded by both this and its
  // own allocation, so the spendable figure is the smaller of the two.
  organizationRemaining: number
}

/**
 * A member's organization balance: how much the organization lets this member spend through
 * company-paid Keys. It is separate from the member's personal balance.
 */
export function UserOrganizationQuotaCell(
  props: UserOrganizationQuotaCellProps
) {
  const { t } = useTranslation()
  useSystemConfigStore((state) => state.config.currency)

  const { meta: currency } = getCurrencyDisplay()
  const quotaUnit = currency.kind === 'tokens' ? t('Tokens') : currency.symbol
  const allocationLeft = props.allocated - props.used
  // The organization can run out before a member's own allocation does, and consumption is
  // checked against both, so report the limit that actually applies.
  const spendable = Math.min(allocationLeft, props.organizationRemaining)
  const formattedSpendable = formatQuotaWithCurrency(spendable, {
    showSymbol: false,
  })
  const formattedUsed = formatQuotaWithCurrency(props.used, {
    showSymbol: false,
  })
  const formattedAllocated = formatQuotaWithCurrency(props.allocated, {
    showSymbol: false,
  })

  return (
    <QuotaDetailsPopover
      title={`${t('Organization allocation')} (${quotaUnit})`}
      triggerLabel={`${t('Organization allocation')} ${t('Spendable quota')} ${formattedSpendable}`}
      details={[
        { label: t('Allocated quota'), value: formattedAllocated },
        { label: t('Total Used'), value: formattedUsed },
        { label: t('Spendable quota'), value: formattedSpendable },
        {
          label: t('Organization'),
          value: formatQuotaWithCurrency(props.organizationRemaining),
        },
      ]}
    >
      <QuotaRow
        label={t('Available')}
        value={formattedSpendable}
        valueClassName={cn(
          spendable < 0 && 'text-destructive',
          spendable === 0 && 'text-muted-foreground'
        )}
        secondaryLabel={t('Used amount')}
        secondaryValue={formattedUsed}
      />
    </QuotaDetailsPopover>
  )
}
