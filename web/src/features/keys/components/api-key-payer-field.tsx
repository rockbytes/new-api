import type { Control } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import type { ApiKeyFormValues } from '../lib/api-key-form'
import {
  organizationIdToPayerValue,
  PERSONAL_PAYER_VALUE,
  payerValueToOrganizationId,
  type PayerOption,
} from '../lib/billing-organization'

type ApiKeyPayerFieldProps = {
  control: Control<ApiKeyFormValues>
  options: PayerOption[]
}

export function ApiKeyPayerField(props: ApiKeyPayerFieldProps) {
  const { t } = useTranslation()

  return (
    <FormField
      control={props.control}
      name='billing_organization_id'
      render={({ field }) => {
        const currentValue = organizationIdToPayerValue(field.value)
        const selectedOption = props.options.find(
          (option) => option.value === currentValue
        )
        return (
          <FormItem>
            <FormLabel>{t('Paid by')}</FormLabel>
            <Select
              value={currentValue}
              onValueChange={(value) =>
                field.onChange(
                  payerValueToOrganizationId(value ?? PERSONAL_PAYER_VALUE)
                )
              }
            >
              <FormControl>
                <SelectTrigger className='w-full'>
                  <SelectValue>{selectedOption?.label}</SelectValue>
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                {props.options.map((option) => (
                  <SelectItem
                    key={option.value}
                    value={option.value}
                    disabled={option.disabled}
                  >
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FormDescription>
              {t(
                'Choose who pays for requests made with this key. Only organizations you belong to are listed.'
              )}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )
      }}
    />
  )
}
