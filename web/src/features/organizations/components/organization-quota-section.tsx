import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  type QuotaAdjustMode,
  QuotaAdjustDialog,
} from '@/components/quota-adjust-dialog'
import { Button } from '@/components/ui/button'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { handleServerError } from '@/lib/handle-server-error'

import { adjustOrganizationQuota, getOrganizationQuota } from '../api'

type OrganizationQuotaSectionProps = {
  organizationId: number
}

// The organization balance is the unallocated pool. Root and platform administrators
// adjust it the same way a user's balance is adjusted.
export function OrganizationQuotaSection(props: OrganizationQuotaSectionProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const organizationId = props.organizationId
  const [adjustOpen, setAdjustOpen] = useState(false)

  const quotaQuery = useQuery({
    queryKey: ['organization-quota', organizationId],
    queryFn: () => getOrganizationQuota(organizationId),
  })

  const refresh = () => {
    queryClient.invalidateQueries({
      queryKey: ['organization-quota', organizationId],
    })
    queryClient.invalidateQueries({
      queryKey: ['organization', organizationId],
    })
  }

  const quota = quotaQuery.data

  const handleAdjust = async (mode: QuotaAdjustMode, value: number) => {
    try {
      await adjustOrganizationQuota(organizationId, {
        mode,
        amount: value,
        reason: '',
        reference: `adjust-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`,
      })
      toast.success(t('Quota adjusted successfully'))
      refresh()
      return true
    } catch (error) {
      handleServerError(error, t('Failed to adjust quota'))
      return false
    }
  }

  return (
    <div className='flex items-end justify-between gap-2'>
      <div className='text-sm'>
        <div className='text-muted-foreground'>{t('Organization balance')}</div>
        <div>{formatQuotaWithCurrency(quota?.unallocated)}</div>
      </div>
      <Button variant='outline' size='sm' onClick={() => setAdjustOpen(true)}>
        {t('Adjust Quota')}
      </Button>
      <QuotaAdjustDialog
        open={adjustOpen}
        onOpenChange={setAdjustOpen}
        currentQuota={quota?.unallocated ?? 0}
        minimumQuota={0}
        onConfirm={handleAdjust}
      />
    </div>
  )
}
