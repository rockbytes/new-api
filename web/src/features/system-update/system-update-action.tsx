import { ChevronUp, RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { ROLE } from '@/lib/roles'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import { SystemUpdateDialog } from './system-update-dialog'
import { useSystemUpdate } from './use-system-update'

type SystemUpdateActionProps = {
  compact?: boolean
}

export function SystemUpdateAction(props: SystemUpdateActionProps) {
  const isAdmin = useAuthStore(
    (state) => (state.auth.user?.role ?? 0) >= ROLE.ADMIN
  )
  if (!isAdmin) return null
  return <AdminSystemUpdateAction {...props} />
}

function AdminSystemUpdateAction(props: SystemUpdateActionProps) {
  const { t } = useTranslation()
  const update = useSystemUpdate()
  const [open, setOpen] = useState(false)
  const compact = props.compact ?? true
  const label = update.shouldNotify
    ? t('Update available')
    : t('Check for updates')
  let description = label
  if (update.shouldNotify) {
    description = t('New version available: {{version}}', {
      version: update.release?.tag_name,
    })
  } else if (update.snapshot?.error) {
    description = t('Failed to check for updates')
  }
  const updateAnnouncement = update.shouldNotify ? description : ''

  const TriggerGlyph = update.shouldNotify ? ChevronUp : RefreshCw

  return (
    <SystemUpdateDialog
      open={open}
      onOpenChange={setOpen}
      update={update}
      trigger={
        <Button
          type='button'
          variant={update.shouldNotify ? 'outline' : 'ghost'}
          aria-label={description}
          title={description}
          aria-busy={update.checking}
          className={cn(
            'relative',
            compact && 'size-8 px-0 lg:w-auto lg:gap-1.5 lg:px-2.5',
            update.shouldNotify &&
              'border-primary/40 bg-primary/10 text-primary'
          )}
        >
          {update.checking ? (
            <Spinner data-icon='inline-start' aria-hidden='true' />
          ) : (
            <TriggerGlyph data-icon='inline-start' aria-hidden='true' />
          )}
          <span className={cn(compact && 'hidden lg:inline')}>{label}</span>
          {update.shouldNotify && compact && (
            <Badge
              aria-hidden='true'
              className='absolute -end-0.5 -top-0.5 size-1.5 min-w-0 p-0 lg:hidden'
            />
          )}
          <span className='sr-only' role='status' aria-live='polite'>
            {updateAnnouncement}
          </span>
        </Button>
      }
    />
  )
}
