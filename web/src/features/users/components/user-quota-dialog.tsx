/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
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
