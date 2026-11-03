import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { GroupBadge, GroupMultiplierBadge } from '@/components/group-badge'
import { cn } from '@/lib/utils'

export type GroupRatio = number | string | null | undefined

export const AUTO_GROUP_FRAME_CLASS_NAME =
  'border-primary/40 relative overflow-visible border shadow-sm shadow-primary/10'

type AutoGroupFrameProps = {
  children: ReactNode
  className?: string
  effect: 'badge' | 'ratio'
  shouldReduceMotion: boolean
}

export function AutoGroupFrame(props: AutoGroupFrameProps) {
  return (
    <span
      data-auto-group-frame='true'
      data-auto-group-effect={props.effect}
      className={cn(
        AUTO_GROUP_FRAME_CLASS_NAME,
        'inline-flex max-w-full shrink-0 rounded-4xl p-px',
        props.className
      )}
    >
      {props.children}
    </span>
  )
}

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

export function AutoGroupBadge(props: { shouldReduceMotion: boolean }) {
  return (
    <AutoGroupFrame
      effect='badge'
      shouldReduceMotion={props.shouldReduceMotion}
    >
      <GroupBadge group='auto' />
    </AutoGroupFrame>
  )
}
