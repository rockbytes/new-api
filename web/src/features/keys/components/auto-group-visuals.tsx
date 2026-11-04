import { useTranslation } from 'react-i18next'

import { GroupMultiplierBadge } from '@/components/group-badge'
import { cn } from '@/lib/utils'

export type GroupRatio = number | string | null | undefined

export const AUTO_GROUP_FRAME_CLASS_NAME =
  'border-primary/40 relative overflow-visible border shadow-sm shadow-primary/10'



type GroupRatioBadgeProps = {
  isAuto?: boolean
  ratio: GroupRatio
  shouldReduceMotion?: boolean
}

export function GroupRatioBadge(props: GroupRatioBadgeProps) {
  const { t } = useTranslation()

  if (props.ratio === undefined || props.ratio === null || props.ratio === '') {
    return null
  }

  return (
    <GroupMultiplierBadge
      ratio={typeof props.ratio === 'number' ? props.ratio : undefined}
      label={typeof props.ratio === 'number' ? undefined : t('Auto')}
      className={cn(
        props.isAuto &&
          'overflow-visible rounded-md border-primary/30 bg-primary/10 text-primary'
      )}
    />
  )
}

