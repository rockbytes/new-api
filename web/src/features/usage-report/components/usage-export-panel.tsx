import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
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

import {
  createUsageExport,
  downloadUsageExport,
  downloadUsageExportDirect,
  getUsageExportEstimate,
  getUsageExports,
  type UsageExportEstimate,
} from '../api'
import {
  USAGE_EXPORT_OPTIONS,
  USAGE_EXPORT_STATUS_BADGES,
  type UsageExportOption,
} from '../constants'
import {
  buildUsageExportRequest,
  hasActiveUsageExports,
  saveUsageExportFile,
} from '../lib/export'
import type {
  UsageExportJob,
  UsageExportRequest,
  UsageQueryParams,
} from '../types'

const EXPORTS_QUERY_KEY = ['usage-report', 'exports']
const POLL_INTERVAL_MS = 3000
const HTTP_GONE = 410

// Names the scope in the confirmation, so the user sees who and what the export covers.
export interface UsageExportScopeLabels {
  organization: string
  member: string
  model: string
}

// An export the user has checked in the dialog: the request sent to the server and how the server
// said it would be delivered.
interface PendingUsageExport {
  option: UsageExportOption
  request: UsageExportRequest
  estimate: UsageExportEstimate
}

export function UsageExportPanel(props: {
  params: UsageQueryParams
  scope: UsageExportScopeLabels
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const queryClient = useQueryClient()
  const [pending, setPending] = useState<PendingUsageExport | null>(null)

  const exportsQuery = useQuery({
    queryKey: EXPORTS_QUERY_KEY,
    queryFn: getUsageExports,
    // Keep polling while the tab is in the background, so a finished job is already up to
    // date when the user returns. Polling stops by itself once no job is active.
    refetchIntervalInBackground: true,
    refetchInterval: (query) =>
      hasActiveUsageExports(query.state.data ?? []) ? POLL_INTERVAL_MS : false,
  })
  const jobs = exportsQuery.data ?? []

  // A workbook job has no kind, so it matches the option whose format is xlsx.
  const exportOptionLabel = (job: UsageExportJob): string => {
    const option = USAGE_EXPORT_OPTIONS.find(
      (candidate) =>
        candidate.format === job.format &&
        (candidate.kind ?? 'workbook') === job.kind
    )
    return option ? t(option.labelKey) : ''
  }

  // Step one: ask the server how the export would be delivered. Nothing is generated yet.
  const estimateMutation = useMutation({
    mutationFn: async (option: UsageExportOption) => {
      const request = buildUsageExportRequest(props.params, option)
      const estimate = await getUsageExportEstimate(request)
      return { option, request, estimate }
    },
    onSuccess: (next) => {
      setPending(next)
    },
    onError: (error) => {
      handleServerError(error, t('The export could not be started'))
    },
  })

  // Step two: the user confirmed. A small export streams to the file the browser saves, and a
  // large one becomes a job that stays in the list.
  const exportMutation = useMutation({
    mutationFn: async (next: PendingUsageExport) => {
      if (next.estimate.delivery === 'queued') {
        await createUsageExport(next.request)
        return 'queued' as const
      }
      const file = await downloadUsageExportDirect(next.request)
      saveUsageExportFile(file.blob, file.fileName)
      return 'direct' as const
    },
    onSuccess: async (delivery) => {
      setPending(null)
      if (delivery === 'queued') {
        toast.success(t('Export queued. Download it from the list when ready.'))
      }
      await queryClient.invalidateQueries({ queryKey: EXPORTS_QUERY_KEY })
    },
    onError: (error) => {
      setPending(null)
      handleServerError(error, t('The export could not be started'))
    },
  })

  const downloadMutation = useMutation({
    mutationFn: async (job: UsageExportJob) => ({
      blob: await downloadUsageExport(job.job_id),
      fileName: job.file_name,
    }),
    onSuccess: ({ blob, fileName }) => {
      saveUsageExportFile(blob, fileName)
    },
    onError: async (error) => {
      // The file was removed after its expiry. Refresh the list so the job reads as expired.
      if (getServerErrorStatus(error) === HTTP_GONE) {
        toast.error(t('This export has expired'))
        await queryClient.invalidateQueries({ queryKey: EXPORTS_QUERY_KEY })
        return
      }
      handleServerError(error, t('The export could not be downloaded'))
    },
  })

  const dateRange = (request: UsageExportRequest): string =>
    `${formatTimestamp(request.start_timestamp)} – ${formatTimestamp(request.end_timestamp)}`

  const deliveryText = (estimate: UsageExportEstimate): string =>
    estimate.delivery === 'queued'
      ? t('Added to the export list. The file is kept for 24 hours.')
      : t('Download now. The file is not kept on the server.')

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex flex-wrap gap-2'>
        {USAGE_EXPORT_OPTIONS.map((option) => (
          <Button
            key={option.key}
            type='button'
            size='sm'
            variant='outline'
            disabled={estimateMutation.isPending || exportMutation.isPending}
            onClick={() => estimateMutation.mutate(option)}
          >
            {t(option.labelKey)}
          </Button>
        ))}
      </div>
      {pending && (
        <ConfirmDialog
          open
          onOpenChange={(open) => {
            if (!open) setPending(null)
          }}
          title={t('Confirm export')}
          desc={t('Check the scope before starting this export.')}
          confirmText={t('Start export')}
          isLoading={exportMutation.isPending}
          handleConfirm={() => exportMutation.mutate(pending)}
        >
          <dl className='grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm'>
            <dt className='text-muted-foreground'>{t('Export')}</dt>
            <dd>{t(pending.option.labelKey)}</dd>
            <dt className='text-muted-foreground'>{t('Time range')}</dt>
            <dd className='tabular-nums'>{dateRange(pending.request)}</dd>
            <dt className='text-muted-foreground'>{t('Organization')}</dt>
            <dd>{props.scope.organization}</dd>
            <dt className='text-muted-foreground'>{t('Member')}</dt>
            <dd>{props.scope.member}</dd>
            <dt className='text-muted-foreground'>{t('Model')}</dt>
            <dd>{props.scope.model}</dd>
            <dt className='text-muted-foreground'>{t('Rows')}</dt>
            <dd className='tabular-nums'>
              {pending.option.key === 'summary'
                ? '-'
                : formatNumber(pending.estimate.row_count, locale)}
            </dd>
            <dt className='text-muted-foreground'>{t('Delivery')}</dt>
            <dd>{deliveryText(pending.estimate)}</dd>
          </dl>
        </ConfirmDialog>
      )}
      {jobs.length === 0 ? (
        <p className='text-muted-foreground text-sm'>{t('No exports yet')}</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Time')}</TableHead>
              <TableHead>{t('Export')}</TableHead>
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
              const hasFile = job.status === 'succeeded'
              return (
                <TableRow key={job.job_id}>
                  <TableCell>{formatTimestamp(job.created_time)}</TableCell>
                  <TableCell>{exportOptionLabel(job)}</TableCell>
                  <TableCell>
                    <StatusBadge
                      label={t(badge.labelKey)}
                      variant={badge.variant}
                      copyable={false}
                    />
                  </TableCell>
                  <TableCell className='text-right tabular-nums'>
                    {job.status === 'succeeded' || job.status === 'expired'
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
      )}
    </div>
  )
}
