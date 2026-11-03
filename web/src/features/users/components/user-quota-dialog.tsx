import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  type QuotaAdjustMode,
  QuotaAdjustDialog,
} from '@/components/quota-adjust-dialog'
import { handleServerError } from '@/lib/handle-server-error'

import { adjustUserQuota } from '../api'

interface UserQuotaDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  userId: number
  currentQuota: number
  onSuccess: () => void
}

export function UserQuotaDialog(props: UserQuotaDialogProps) {
  const { t } = useTranslation()

  const handleConfirm = async (mode: QuotaAdjustMode, value: number) => {
    try {
      const result = await adjustUserQuota({
        id: props.userId,
        action: 'add_quota',
        mode,
        value: mode === 'override' ? value : Math.abs(value),
      })
      if (result.success) {
        toast.success(t('Quota adjusted successfully'))
        props.onSuccess()
        return true
      }
      handleServerError(result, t('Failed to adjust quota'))
      return false
    } catch (e: unknown) {
      handleServerError(e, t('Failed to adjust quota'))
      return false
    }
  }

  return (
    <QuotaAdjustDialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      currentQuota={props.currentQuota}
      onConfirm={handleConfirm}
    />
  )
}
