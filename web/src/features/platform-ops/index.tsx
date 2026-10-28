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
import { Eye, EyeOff } from 'lucide-react'
import { useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { FlowCharts } from '@/features/dashboard/components/flow/flow-charts'
import { ModelsFilter } from '@/features/dashboard/components/models/models-filter-dialog'
import {
  buildDefaultDashboardFilters,
  getSavedChartPreferences,
} from '@/features/dashboard/lib'
import type { DashboardFilters } from '@/features/dashboard/types'

// Operational traffic, from users through tokens and groups to channels and models. It is
// an administrator view, so it lives outside the business dashboard.
export function PlatformOps() {
  const { t } = useTranslation()
  const [chartPreferences] = useState(() => getSavedChartPreferences())
  const [filters, setFilters] = useState<DashboardFilters>(() =>
    buildDefaultDashboardFilters(getSavedChartPreferences())
  )
  const [sensitiveVisible, setSensitiveVisible] = useState(true)

  const handleFilterChange = useCallback((next: DashboardFilters) => {
    setFilters(next)
  }, [])

  const handleResetFilters = useCallback(() => {
    setFilters(buildDefaultDashboardFilters(chartPreferences))
  }, [chartPreferences])

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Platform operations')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <div className='flex items-center gap-1'>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant='ghost'
                  size='icon'
                  onClick={() => setSensitiveVisible((prev) => !prev)}
                  aria-label={
                    sensitiveVisible
                      ? t('Hide sensitive data')
                      : t('Show sensitive data')
                  }
                  className='text-muted-foreground hover:text-foreground size-8'
                />
              }
            >
              {sensitiveVisible ? <Eye /> : <EyeOff />}
            </TooltipTrigger>
            <TooltipContent>
              {sensitiveVisible
                ? t('Hide sensitive data')
                : t('Show sensitive data')}
            </TooltipContent>
          </Tooltip>
          <ModelsFilter
            preferences={chartPreferences}
            currentFilters={filters}
            onFilterChange={handleFilterChange}
            onReset={handleResetFilters}
            titleKey='Flow Filters'
            descriptionKey='Filter the traffic flow view by time range and user.'
          />
        </div>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <p className='text-muted-foreground mb-4 text-sm'>
          {t('Traffic and channel operations, visible to administrators only.')}
        </p>
        <FlowCharts filters={filters} sensitiveVisible={sensitiveVisible} />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
