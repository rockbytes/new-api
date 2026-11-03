import { Rows3, Rows4 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import {
  DATA_TABLE_DENSITIES,
  useDataTableDensity,
} from '../hooks/use-data-table-density'

/**
 * Switches every data table between comfortable and compact row heights.
 * The toggle is pressed while compact rows are active.
 */
export function DataTableDensityToggle() {
  const { t } = useTranslation()
  const [density, setDensity] = useDataTableDensity()
  const isCompact = density === DATA_TABLE_DENSITIES.COMPACT
  const label = isCompact ? t('Comfortable rows') : t('Compact rows')

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant='ghost'
            size='icon-sm'
            aria-label={label}
            aria-pressed={isCompact}
            onClick={() =>
              setDensity(
                isCompact
                  ? DATA_TABLE_DENSITIES.COMFORTABLE
                  : DATA_TABLE_DENSITIES.COMPACT
              )
            }
          />
        }
      >
        {isCompact ? (
          <Rows4 className='size-4' />
        ) : (
          <Rows3 className='size-4' />
        )}
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}
