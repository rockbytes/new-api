import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber, formatTimestamp } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { getServerErrorStatus } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import {
  createUsageExport,
  downloadUsageExport,
  downloadUsageExportDirect,
  getUsageExportEstimate,
  getUsageExports,
} from '../api'
import { USAGE_EXPORT_STATUS_BADGES } from '../constants'
import {
  buildUsageExportRequest,
  hasActiveUsageExports,
  saveUsageExportFile,
} from '../lib/export'
import type {
  UsageExportFormat,
  UsageExportJob,
  UsageExportRequest,
  UsageQueryParams,
} from '../types'

const EXPORTS_QUERY_KEY = ['usage-report', 'exports']
const POLL_INTERVAL_MS = 3000
const HTTP_GONE = 410

// Names the scope in the confirmation, so the user sees who and what the export covers.
export interface UsageExportScopeLabels {
  payer?: string
  organization: string
  member: string
  model: string
}

export function UsageExportAction(props: {
  params: UsageQueryParams
  scope: UsageExportScopeLabels
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const [selecting, setSelecting] = useState(false)
  const [format, setFormat] = useState<UsageExportFormat>('xlsx')

  // After confirmation, estimate delivery and then download or queue the same request.
  const exportMutation = useMutation({
    mutationFn: async (request: UsageExportRequest) => {
      const estimate = await getUsageExportEstimate(request)
      if (estimate.delivery === 'queued') {
        await createUsageExport(request)
        return 'queued' as const
      }
      const file = await downloadUsageExportDirect(request)
      saveUsageExportFile(file.blob, file.fileName)
      return 'direct' as const
    },
    onSuccess: async (delivery) => {
      setSelecting(false)
      if (delivery === 'queued') {
        toast.success(t('Export queued. Download it from the list when ready.'))
      }
      const queryKey = [...EXPORTS_QUERY_KEY, userId]
      await queryClient.cancelQueries({ queryKey })
      await queryClient.invalidateQueries({ queryKey })
    },
    onError: (error) => {
      setSelecting(false)
      handleServerError(error, t('The export could not be started'))
    },
  })

  const dateRange = (request: UsageExportRequest): string =>
    `${formatTimestamp(request.start_timestamp)} – ${formatTimestamp(request.end_timestamp)}`

  return (
    <>
      <Button
        type='button'
        size='sm'
        variant='outline'
        disabled={exportMutation.isPending}
        onClick={() => {
          setFormat('xlsx')
          setSelecting(true)
        }}
      >
        {t('Export usage summary')}
      </Button>
      <ConfirmDialog
        open={selecting}
        onOpenChange={(open) => {
          if (!exportMutation.isPending) setSelecting(open)
        }}
        title={t('Export usage summary')}
        desc={t('Check the scope before starting this export.')}
        confirmText={
          exportMutation.isPending ? t('Loading...') : t('Start export')
        }
        isLoading={exportMutation.isPending}
        handleConfirm={() =>
          exportMutation.mutate(buildUsageExportRequest(props.params, format))
        }
      >
        <dl className='grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm'>
          <dt className='text-muted-foreground'>{t('Time range')}</dt>
          <dd className='tabular-nums'>
            {dateRange(buildUsageExportRequest(props.params, format))}
          </dd>
          {props.scope.payer && (
            <>
              <dt className='text-muted-foreground'>{t('Paid by')}</dt>
              <dd>{props.scope.payer}</dd>
            </>
          )}
          <dt className='text-muted-foreground'>{t('Organization')}</dt>
          <dd>{props.scope.organization}</dd>
          <dt className='text-muted-foreground'>{t('Member')}</dt>
          <dd>{props.scope.member}</dd>
          <dt className='text-muted-foreground'>{t('Model')}</dt>
          <dd>{props.scope.model}</dd>
        </dl>
        <RadioGroup
          value={format}
          onValueChange={(value) => {
            if (value === 'xlsx' || value === 'csv') setFormat(value)
          }}
          aria-label={t('Format')}
        >
          <div className='flex items-center gap-2'>
            <RadioGroupItem
              id='usage-export-xlsx'
              value='xlsx'
              disabled={exportMutation.isPending}
            />
            <Label htmlFor='usage-export-xlsx'>XLSX</Label>
          </div>
          <div className='flex items-center gap-2'>
            <RadioGroupItem
              id='usage-export-csv'
              value='csv'
              disabled={exportMutation.isPending}
            />
            <Label htmlFor='usage-export-csv'>CSV</Label>
          </div>
        </RadioGroup>
      </ConfirmDialog>
    </>
  )
}

export function UsageExportHistory() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const queryClient = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const exportsQueryKey = [...EXPORTS_QUERY_KEY, userId]

  const exportsQuery = useQuery({
    queryKey: exportsQueryKey,
    queryFn: ({ signal }) => getUsageExports(signal),
    // Keep polling while the tab is in the background, so a finished job is already up to
    // date when the user returns. Polling stops by itself once no job is active.
    refetchIntervalInBackground: true,
    refetchInterval: (query) =>
      hasActiveUsageExports(query.state.data ?? []) ? POLL_INTERVAL_MS : false,
  })
  const jobs = exportsQuery.data ?? []

  const exportOptionLabel = (job: UsageExportJob): string => {
    if (job.kind === 'workbook') return t('Usage workbook (XLSX)')
    if (job.kind === 'records') return t('Usage records (CSV)')
    return job.format === 'xlsx'
      ? `${t('Export usage summary')} (XLSX)`
      : t('Usage summary (CSV)')
  }

  const downloadMutation = useMutation({
    mutationFn: async (job: UsageExportJob) => ({
      blob: await downloadUsageExport(job.job_id, job.payer_type),
      fileName: job.file_name,
    }),
    onSuccess: ({ blob, fileName }) => {
      saveUsageExportFile(blob, fileName)
    },
    onError: async (error) => {
      // The file was removed after its expiry. Refresh the list so the job reads as expired.
      if (getServerErrorStatus(error) === HTTP_GONE) {
        toast.error(t('This export has expired'))
        await queryClient.invalidateQueries({ queryKey: exportsQueryKey })
        return
      }
      handleServerError(error, t('The export could not be downloaded'))
    },
  })

  if (jobs.length === 0) {
    return (
      <p className='text-muted-foreground text-sm'>{t('No exports yet')}</p>
    )
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('Time')}</TableHead>
          <TableHead>{t('Export')}</TableHead>
          <TableHead>{t('Paid by')}</TableHead>
          <TableHead>{t('Status')}</TableHead>
          <TableHead className='text-right'>{t('Rows')}</TableHead>
          <TableHead>{t('Expires')}</TableHead>
          <TableHead>
            <span className='sr-only'>{t('Action')}</span>
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {jobs.map((job) => {
          const badge = USAGE_EXPORT_STATUS_BADGES[job.status]
          let payer = t('Unknown')
          if (job.payer_type === 'personal') payer = t('Personal')
          if (job.payer_type === 'organization') payer = t('Organization')
          const hasFile = job.status === 'succeeded'
          return (
            <TableRow key={job.job_id}>
              <TableCell>{formatTimestamp(job.created_time)}</TableCell>
              <TableCell>{exportOptionLabel(job)}</TableCell>
              <TableCell>{payer}</TableCell>
              <TableCell>
                <StatusBadge
                  label={t(badge.labelKey)}
                  variant={badge.variant}
                  copyable={false}
                />
              </TableCell>
              <TableCell className='text-right tabular-nums'>
                {hasFile ||
                job.status === 'expired' ||
                job.status === 'direct_downloaded'
                  ? formatNumber(job.row_count, locale)
                  : '-'}
              </TableCell>
              <TableCell className='whitespace-nowrap'>
                {hasFile ? formatTimestamp(job.expires_time) : '-'}
              </TableCell>
              <TableCell className='text-right'>
                {hasFile && (
                  <Button
                    type='button'
                    size='sm'
                    variant='outline'
                    disabled={downloadMutation.isPending}
                    onClick={() => downloadMutation.mutate(job)}
                  >
                    {t('Download')}
                  </Button>
                )}
              </TableCell>
            </TableRow>
          )
        })}
      </TableBody>
    </Table>
  )
}
