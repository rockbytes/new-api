import { useTranslation } from 'react-i18next'

import { QuotaDetailsPopover } from '@/components/quota-details-popover'
import { formatQuotaWithCurrency, getCurrencyDisplay } from '@/lib/currency'
import { cn } from '@/lib/utils'
import { useSystemConfigStore } from '@/stores/system-config-store'

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
      <span className='grid min-w-0 grid-cols-1 gap-y-1 text-sm tabular-nums'>
        <span className='text-muted-foreground text-xs font-normal'>
          {t('Organization allocation')}
        </span>
        <span
          className={cn(
            remaining < 0 && 'text-destructive',
            remaining === 0 && 'text-muted-foreground'
          )}
        >
          {formattedRemaining}
        </span>
        <span
          data-table-text='secondary'
          className='text-muted-foreground flex items-baseline gap-1 text-xs font-normal'
        >
          <span>{t('Allocated quota')}</span>
          <span>{formattedAllocated}</span>
        </span>
      </span>
    </QuotaDetailsPopover>
  )
}
