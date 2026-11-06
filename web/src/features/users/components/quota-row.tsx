import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

type QuotaRowProps = {
  /** Which balance the row shows, for example Personal or Organization. */
  label: string
  /** The main amount. Omit it when the row shows a badge instead. */
  value?: string
  valueClassName?: string
  badge?: ReactNode
  /** A secondary figure beside the main amount, for example used or allocated quota. */
  secondaryLabel?: string
  secondaryValue?: string
}

/**
 * One balance in the users table. The label, the main amount, and the secondary figure sit on a
 * single line, so the personal and organization balances read as two separate rows.
 */
export function QuotaRow(props: QuotaRowProps) {
  return (
    <span className='grid min-w-0 grid-cols-[3.5rem_minmax(0,1fr)] items-baseline gap-x-2 text-sm tabular-nums'>
      <span className='text-muted-foreground text-xs'>{props.label}</span>
      <span className='flex min-w-0 flex-wrap items-baseline gap-x-2 gap-y-0.5'>
        {props.value !== undefined && (
          <span className={cn('font-medium', props.valueClassName)}>
            {props.value}
          </span>
        )}
        {props.badge}
        {props.secondaryLabel !== undefined && (
          <span
            data-table-text='secondary'
            className='text-muted-foreground flex items-baseline gap-1 text-xs font-normal'
          >
            <span>{props.secondaryLabel}</span>
            <span>{props.secondaryValue}</span>
          </span>
        )}
      </span>
    </span>
  )
}
