import { useTranslation } from 'react-i18next'

import { getGatewayFeatures } from '../constants'

interface GatewayCardProps {
  logo: string
  systemName: string
}

/**
 * Central gateway card with features grid
 */
export function GatewayCard({ logo, systemName }: GatewayCardProps) {
  const { t } = useTranslation()
  const features = getGatewayFeatures(t)

  return (
    <div className='bg-card group border-border dark:border-border/20 relative overflow-hidden rounded-4xl border p-10 shadow-2xl transition-all duration-500 sm:p-12 dark:shadow-[0_25px_80px_-15px_rgba(0,0,0,0.4)]'>
      <div className='relative'>
        {/* Gateway Header */}
        <div className='mb-8 flex items-center justify-center gap-3'>
          <img
            src={logo}
            alt={systemName}
            className='h-12 w-12 rounded-lg object-cover'
          />
          <h3 className='text-foreground text-2xl font-bold'>{systemName}</h3>
        </div>

        {/* Features Grid */}
        <div className='grid grid-cols-2 gap-3'>
          {features.map((feature) => (
            <div
              key={feature}
              className='bg-muted/30 group/item border-border/40 dark:border-border/20 relative overflow-hidden rounded-xl border px-4 py-3.5 text-center shadow-sm transition-all duration-300 hover:scale-[1.02] hover:border-amber-500/40 hover:shadow-md'
            >
              <span className='text-foreground/90 group-hover/item:text-foreground relative text-sm font-medium'>
                {feature}
              </span>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
