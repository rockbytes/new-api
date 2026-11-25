import { ChevronDown, RotateCcw } from 'lucide-react'
import { memo, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { cn } from '@/lib/utils'

import { ENDPOINT_TYPES, FILTER_ALL, getEndpointTypeLabels } from '../constants'
import type { PricingModel } from '../types'

type FilterOption = {
  value: string
  label: string
  count: number
}

type FilterSectionProps = {
  title: string
  value: string
  options: FilterOption[]
  onChange: (value: string) => void
}

export interface PricingSidebarProps {
  endpointTypeFilter: string
  groupFilter: string
  onEndpointTypeChange: (value: string) => void
  onGroupChange: (value: string) => void
  groupCounts: ReadonlyMap<string, number>
  totalCount: number
  endpointCountsModels: PricingModel[]
  hasActiveFilters: boolean
  onClearFilters: () => void
  className?: string
}

function FilterChip(props: {
  option: FilterOption
  active: boolean
  onClick: () => void
}) {
  const { i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)

  return (
    <Button
      type='button'
      variant={props.active ? 'secondary' : 'outline'}
      size='sm'
      onClick={props.onClick}
      aria-pressed={props.active}
      className='h-auto max-w-full gap-1.5 px-2 py-1 text-xs'
      title={props.option.label}
    >
      <span className='truncate'>{props.option.label}</span>
      <span
        className={cn(
          'rounded-md px-1.5 py-0.5 text-[12px]',
          props.active
            ? 'bg-background text-foreground'
            : 'bg-muted text-muted-foreground'
        )}
      >
        {formatNumber(props.option.count, locale)}
      </span>
    </Button>
  )
}

function FilterSection(props: FilterSectionProps) {
  return (
    <Collapsible
      defaultOpen
      className='border-border/70 border-b pb-3 last:border-b-0'
    >
      <CollapsibleTrigger className='group flex w-full items-center justify-between py-2.5 text-left'>
        <span className='text-foreground text-sm font-semibold'>
          {props.title}
        </span>
        <ChevronDown className='text-muted-foreground size-4 transition-transform group-data-[panel-open]:rotate-180' />
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className='flex flex-wrap gap-1.5'>
          {props.options.map((option) => (
            <FilterChip
              key={option.value}
              option={option}
              active={props.value === option.value}
              onClick={() => props.onChange(option.value)}
            />
          ))}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}

export const PricingSidebar = memo(function PricingSidebar(
  props: PricingSidebarProps
) {
  const { t } = useTranslation()
  const endpointCounts = useMemo(() => {
    const endpoints = new Map<string, number>()
    for (const model of props.endpointCountsModels) {
      for (const endpoint of new Set(model.supported_endpoint_types ?? [])) {
        endpoints.set(endpoint, (endpoints.get(endpoint) ?? 0) + 1)
      }
    }
    return endpoints
  }, [props.endpointCountsModels])
  const endpointTypeLabels = getEndpointTypeLabels(t)

  const groupOptions: FilterOption[] = [
    { value: FILTER_ALL, label: t('All Groups'), count: props.totalCount },
    ...Array.from(props.groupCounts, ([group, count]) => ({
      value: group,
      label: group,
      count,
    })).sort((a, b) => a.label.localeCompare(b.label)),
  ]

  const endpointOptions: FilterOption[] = [
    {
      value: ENDPOINT_TYPES.ALL,
      label: endpointTypeLabels[ENDPOINT_TYPES.ALL],
      count: props.endpointCountsModels.length,
    },
    ...Object.entries(endpointTypeLabels)
      .filter(([value]) => value !== ENDPOINT_TYPES.ALL)
      .map(([value, label]) => ({
        value,
        label,
        count: endpointCounts.get(value) ?? 0,
      })),
  ]

  return (
    <aside className={cn('bg-card rounded-xl border p-3', props.className)}>
      <div className='mb-2.5 flex items-center justify-between gap-2'>
        <div>
          <h2 className='text-foreground text-sm font-bold'>{t('Filter')}</h2>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t('Filter models by channel group and endpoint type.')}
          </p>
        </div>
        <Button
          type='button'
          variant='ghost'
          size='sm'
          onClick={props.onClearFilters}
          disabled={!props.hasActiveFilters}
          className='h-7 gap-1.5 px-2 text-xs'
        >
          <RotateCcw className='size-3.5' />
          {t('Reset')}
        </Button>
      </div>

      {props.hasActiveFilters && (
        <Badge variant='secondary' className='mb-3'>
          {t('Filters active')}
        </Badge>
      )}

      <div className='space-y-1'>
        {props.groupCounts.size > 0 && (
          <FilterSection
            title={t('Channel Groups')}
            value={props.groupFilter}
            options={groupOptions}
            onChange={props.onGroupChange}
          />
        )}
        <FilterSection
          title={t('Endpoint Type')}
          value={props.endpointTypeFilter}
          options={endpointOptions}
          onChange={props.onEndpointTypeChange}
        />
      </div>
    </aside>
  )
})
