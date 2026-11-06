import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

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
import type { UsageExportJob, UsageQueryParams } from '../types'

const EXPORTS_QUERY_KEY = ['usage-report', 'exports']
const POLL_INTERVAL_MS = 3000
const HTTP_GONE = 410

export function UsageExportPanel(props: { params: UsageQueryParams }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const queryClient = useQueryClient()

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

  // The server decides how the export is delivered. A small one streams straight to the file
  // the browser saves. A large one becomes a job that stays in the list for a day.
  const exportMutation = useMutation({
    mutationFn: async (option: UsageExportOption) => {
      const request = buildUsageExportRequest(props.params, option)
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
      if (delivery === 'queued') {
        toast.success(t('Export queued. Download it from the list when ready.'))
      }
      await queryClient.invalidateQueries({ queryKey: EXPORTS_QUERY_KEY })
    },
    onError: (error) => {
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

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex flex-wrap gap-2'>
        {USAGE_EXPORT_OPTIONS.map((option) => (
          <Button
            key={option.key}
            type='button'
            size='sm'
            variant='outline'
            disabled={exportMutation.isPending}
            onClick={() => exportMutation.mutate(option)}
          >
            {t(option.labelKey)}
          </Button>
        ))}
      </div>
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
