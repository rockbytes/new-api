import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

type QuotaRowProps = {
  /** Labels the main amount, for example the word for a spendable balance. Omit it for a badge. */
  label?: string
  /** The main amount. Omit it when the row shows a badge instead. */
  value?: string
  valueClassName?: string
  badge?: ReactNode
  /** A secondary figure under the main amount, for example the amount already used. */
  secondaryLabel?: string
  secondaryValue?: string
}

/**
 * One balance in the users table. Both figures carry their own word, so the column holding it
 * does not need a separate label and the row stays readable at a glance.
 */
export function QuotaRow(props: QuotaRowProps) {
  return (
    <span className='grid min-w-0 gap-y-0.5 text-sm tabular-nums'>
      <span className='flex min-w-0 items-baseline gap-1.5'>
        {props.label !== undefined && (
          <span className='text-muted-foreground shrink-0 text-xs'>
            {props.label}
          </span>
        )}
        {props.value !== undefined && (
          <span className={cn('font-medium break-all', props.valueClassName)}>
            {props.value}
          </span>
        )}
        {props.badge}
      </span>
      {props.secondaryLabel !== undefined && (
        <span
          data-table-text='secondary'
          className='text-muted-foreground flex min-w-0 items-baseline gap-1.5 text-xs font-normal'
        >
          <span className='shrink-0'>{props.secondaryLabel}</span>
          <span className='min-w-0 break-all'>{props.secondaryValue}</span>
        </span>
      )}
    </span>
  )
}
