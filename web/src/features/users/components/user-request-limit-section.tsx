import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { sideDrawerSectionClassName } from '@/components/drawer-layout'
import { Button } from '@/components/ui/button'

import { getUserRequestLimit } from '../api'
import { UserRequestLimitDialog } from './user-request-limit-dialog'

type UserRequestLimitSectionProps = {
  userId: number
  // False when the signed-in administrator may not change this user's limits.
  canManage: boolean
}

// Shows the request limits that apply to a user, and lets an administrator who
// may manage the user change them.
export function UserRequestLimitSection(props: UserRequestLimitSectionProps) {
  const { t } = useTranslation()
  const [dialogOpen, setDialogOpen] = useState(false)

  const limitQuery = useQuery({
    queryKey: ['user-request-limit', props.userId],
    queryFn: () => getUserRequestLimit(props.userId),
  })
  const limit = limitQuery.data
  const hasOverride = limit
    ? limit.total_count > 0 || limit.success_count > 0
    : false

  return (
    <section className={sideDrawerSectionClassName()}>
      <h3 className='text-sm font-medium'>{t('Request limits')}</h3>
      {limit ? (
        <>
          <p className='text-muted-foreground text-sm'>
            {t('Total requests per window: {{count}}', {
              count:
                limit.effective_total > 0
                  ? limit.effective_total
                  : t('not limited'),
            })}
          </p>
          <p className='text-muted-foreground text-sm'>
            {t('Successful requests per window: {{count}}', {
              count:
                limit.effective_success > 0
                  ? limit.effective_success
                  : t('not limited'),
            })}
          </p>
          <p className='text-muted-foreground text-sm'>
            {hasOverride
              ? t('This user has its own limits.')
              : t('This user uses the global defaults.')}
          </p>
        </>
      ) : null}
      {props.canManage ? null : (
        <p className='text-muted-foreground text-sm'>
          {t(
            "Only administrators above this user's role can change its limits."
          )}
        </p>
      )}
      <div>
        <Button
          type='button'
          variant='outline'
          disabled={!props.canManage || !limit}
          onClick={() => setDialogOpen(true)}
        >
          {t('Edit request limits')}
        </Button>
      </div>
      {dialogOpen && limit ? (
        // Mounted only while open, so the form starts from the stored values.
        <UserRequestLimitDialog
          open
          onOpenChange={setDialogOpen}
          userId={props.userId}
          limit={limit}
        />
      ) : null}
    </section>
  )
}
