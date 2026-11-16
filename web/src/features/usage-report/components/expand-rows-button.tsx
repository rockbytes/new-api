import { ChevronDown, ChevronUp } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

export function ExpandRowsButton(props: {
  expanded: boolean
  hiddenCount: number
  onToggle: () => void
}) {
  const { t } = useTranslation()
  return (
    <Button
      type='button'
      variant='ghost'
      size='sm'
      className='mt-2 w-fit gap-1'
      aria-expanded={props.expanded}
      onClick={props.onToggle}
    >
      {props.expanded
        ? t('Show less')
        : t('Show {{count}} more', { count: props.hiddenCount })}
      {props.expanded ? (
        <ChevronUp className='size-3.5' aria-hidden='true' />
      ) : (
        <ChevronDown className='size-3.5' aria-hidden='true' />
      )}
    </Button>
  )
}
