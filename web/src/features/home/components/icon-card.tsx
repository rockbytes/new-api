import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'

interface IconCardProps {
  iconName: string
  size?: number
  className?: string
}

/**
 * Reusable icon card component with glass morphism effect
 */
export function IconCard({ iconName, size = 32, className }: IconCardProps) {
  return (
    <div
      className={cn(
        'bg-card group/card border-border',
        'relative overflow-hidden rounded-2xl border p-5',
        'transition-colors duration-300',
        className
      )}
    >
      <div className='relative flex items-center justify-center'>
        {getLobeIcon(iconName, size)}
      </div>
    </div>
  )
}
