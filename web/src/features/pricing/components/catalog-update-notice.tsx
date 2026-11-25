import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'

export function CatalogUpdateNotice(props: {
  isFetching: boolean
  onRetry: () => void
}) {
  const { t } = useTranslation()

  return (
    <Alert>
      <AlertDescription className='flex flex-wrap items-center justify-between gap-3'>
        {t(
          'Model catalog is being updated. Group endpoints are temporarily unavailable.'
        )}
        <Button
          variant='outline'
          size='sm'
          disabled={props.isFetching}
          onClick={props.onRetry}
        >
          {t('Retry')}
        </Button>
      </AlertDescription>
    </Alert>
  )
}
