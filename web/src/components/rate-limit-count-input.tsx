import type { ComponentProps } from 'react'
import { useTranslation } from 'react-i18next'

import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import { RATE_LIMIT_COUNT_MAX } from '@/lib/rate-limits'

type RateLimitCountInputProps = Omit<
  ComponentProps<typeof InputGroupInput>,
  'value' | 'onChange' | 'type' | 'min' | 'max' | 'step'
> & {
  value: number
  onChange: (value: number) => void
}

export function RateLimitCountInput({
  value,
  onChange,
  ...props
}: RateLimitCountInputProps) {
  const { t } = useTranslation()

  return (
    <InputGroup>
      <InputGroupInput
        {...props}
        type='number'
        min={0}
        max={RATE_LIMIT_COUNT_MAX}
        step={1}
        value={Number.isFinite(value) ? value : ''}
        onChange={(event) => onChange(event.target.valueAsNumber)}
      />
      {value === 0 && (
        <InputGroupAddon align='inline-end'>{t('Unlimited')}</InputGroupAddon>
      )}
    </InputGroup>
  )
}
