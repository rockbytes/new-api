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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { getCurrencyDisplay, getCurrencyLabel } from '@/lib/currency'
import { formatQuota, parseQuotaFromDollars } from '@/lib/format'
import { cn } from '@/lib/utils'

export type QuotaAdjustMode = 'add' | 'subtract' | 'override'

interface QuotaAdjustDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentQuota: number
  // When set, a change that would leave the balance below this value is refused in the dialog.
  minimumQuota?: number
  // Receives the mode and the quota value: the amount for add and subtract, the target for override.
  // Resolves to true when the change was applied, which closes the dialog.
  onConfirm: (mode: QuotaAdjustMode, value: number) => Promise<boolean>
}

// Adds, subtracts, or overrides a quota balance. The caller decides what the balance is and
// how the change is sent.
export function QuotaAdjustDialog(props: QuotaAdjustDialogProps) {
  const { t } = useTranslation()
  const [mode, setMode] = useState<QuotaAdjustMode>('add')
  const [amount, setAmount] = useState('')
  const [loading, setLoading] = useState(false)

  const { meta: currencyMeta } = getCurrencyDisplay()
  const currencyLabel = getCurrencyLabel()
  const tokensOnly = currencyMeta.kind === 'tokens'

  const amountValue = Number.parseFloat(amount) || 0
  const quotaValue = parseQuotaFromDollars(Math.abs(amountValue))
  const overrideValue = parseQuotaFromDollars(amountValue)
  const getNextQuota = (): number => {
    switch (mode) {
      case 'add':
        return props.currentQuota + quotaValue
      case 'subtract':
        return props.currentQuota - quotaValue
      case 'override':
        return overrideValue
    }
  }
  const belowMinimum =
    props.minimumQuota !== undefined && getNextQuota() < props.minimumQuota

  const getPreviewText = () => {
    const current = props.currentQuota
    // A result below the minimum is not shown as a number; the warning explains why.
    if (belowMinimum) return `${t('Current quota')}: ${formatQuota(current)}`
    switch (mode) {
      case 'add':
        return `${t('Current quota')}: ${formatQuota(current)}  +${formatQuota(quotaValue)} = ${formatQuota(current + quotaValue)}`
      case 'subtract':
        return `${t('Current quota')}: ${formatQuota(current)}  -${formatQuota(quotaValue)} = ${formatQuota(current - quotaValue)}`
      case 'override':
        return `${t('Current quota')}: ${formatQuota(current)} → ${formatQuota(parseQuotaFromDollars(amountValue))}`
      default:
        return ''
    }
  }

  const reset = () => {
    setAmount('')
    setMode('add')
  }

  const handleConfirm = async () => {
    if (!amount && mode !== 'override') return
    if (quotaValue <= 0 && mode !== 'override') return
    if (belowMinimum) return

    setLoading(true)
    try {
      const value = mode === 'override' ? overrideValue : quotaValue
      const applied = await props.onConfirm(mode, value)
      if (applied) {
        reset()
        props.onOpenChange(false)
      }
    } finally {
      setLoading(false)
    }
  }

  const handleCancel = () => {
    reset()
    props.onOpenChange(false)
  }

  const placeholder = tokensOnly
    ? t('Enter amount in tokens')
    : t('Enter amount in {{currency}}', { currency: currencyLabel })

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Adjust Quota')}
      description={t('Select an operation mode and enter the amount')}
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button variant='outline' onClick={handleCancel}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleConfirm} disabled={loading || belowMinimum}>
            {loading ? t('Processing...') : t('Confirm')}
          </Button>
        </>
      }
    >
      <div className='space-y-4'>
        <div className='text-muted-foreground text-sm'>{getPreviewText()}</div>
        {belowMinimum && (
          <div className='text-destructive text-sm'>
            {t('Amount exceeds the available quota.')}
          </div>
        )}

        <div className='space-y-2'>
          <Label>{t('Mode')}</Label>
          <div className='flex gap-1'>
            {(['add', 'subtract', 'override'] as const).map((m) => (
              <Button
                key={m}
                type='button'
                variant='outline'
                size='sm'
                className={cn(
                  mode === m &&
                    'bg-primary text-primary-foreground hover:bg-primary/90 hover:text-primary-foreground'
                )}
                onClick={() => {
                  setMode(m)
                  setAmount('')
                }}
              >
                {m === 'add' && t('Add')}
                {m === 'subtract' && t('Subtract')}
                {m === 'override' && t('Override')}
              </Button>
            ))}
          </div>
        </div>

        <div className='space-y-2'>
          <Label>
            {t('Amount')} ({currencyLabel})
          </Label>
          <Input
            type='number'
            step={tokensOnly ? 1 : 0.000001}
            min={mode === 'override' ? undefined : 0}
            placeholder={placeholder}
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') handleConfirm()
            }}
          />
        </div>
      </div>
    </Dialog>
  )
}
