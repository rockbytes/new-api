import { Loader2 } from 'lucide-react'

import { cn } from '@/lib/utils'

type SpinnerProps = React.ComponentProps<typeof Loader2>

function Spinner({ className, ...props }: SpinnerProps) {
  return (
    <Loader2
      role='status'
      aria-label='Loading'
      className={cn('size-4 animate-spin', className)}
      {...props}
    />
  )
}

export { Spinner }
