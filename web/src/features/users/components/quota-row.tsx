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
 * One balance in the users table. The label and the main amount share the first line. The
 * secondary figure always sits on its own line, aligned under the amount, so every row has the
 * same structure whatever the width of its numbers.
 */
export function QuotaRow(props: QuotaRowProps) {
  return (
    <span className='grid min-w-0 grid-cols-[3.5rem_minmax(0,1fr)] items-baseline gap-x-2 gap-y-0.5 text-sm tabular-nums'>
      <span className='text-muted-foreground text-xs'>{props.label}</span>
      {(props.value !== undefined || props.badge) && (
        <span className='flex min-w-0 items-baseline gap-2'>
          {props.value !== undefined && (
            <span className={cn('font-medium break-all', props.valueClassName)}>
              {props.value}
            </span>
          )}
          {props.badge}
        </span>
      )}
      {props.secondaryLabel !== undefined && (
        <span
          data-table-text='secondary'
          className='text-muted-foreground col-start-2 flex min-w-0 items-baseline gap-1 text-xs font-normal'
        >
          <span className='shrink-0'>{props.secondaryLabel}</span>
          <span className='min-w-0 break-all'>{props.secondaryValue}</span>
        </span>
      )}
    </span>
  )
}
