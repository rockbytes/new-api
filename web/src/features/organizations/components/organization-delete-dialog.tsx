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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatNumber } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import { deleteOrganization, getOrganizationDeletionImpact } from '../api'

// Labels for the blocker codes returned by the server. Unknown codes are shown as they are.
const BLOCKER_LABEL_KEYS: Record<string, string> = {
  status_not_suspended: 'Suspend the organization before deleting it.',
  members_remaining: 'Remove the remaining members.',
  unallocated_quota_remaining:
    'Unallocated quota must be zero before deletion.',
  enabled_paid_keys: 'Disable the enabled organization keys.',
}

type OrganizationDeleteDialogProps = {
  organizationId: number
  open: boolean
  onOpenChange: (open: boolean) => void
  onDeleted: () => void
}

export function OrganizationDeleteDialog(props: OrganizationDeleteDialogProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const queryClient = useQueryClient()
  const impactKey = ['organization', props.organizationId, 'deletion-impact']

  const impactQuery = useQuery({
    queryKey: impactKey,
    queryFn: () => getOrganizationDeletionImpact(props.organizationId),
    enabled: props.open,
  })

  const deleteMutation = useMutation({
    mutationFn: () => deleteOrganization(props.organizationId),
    onSuccess: () => {
      toast.success(t('Organization deleted.'))
      queryClient.invalidateQueries({ queryKey: ['organizations'] })
      props.onDeleted()
    },
    onError: (error) => {
      handleServerError(error, t('Failed to delete organization.'))
      // The server re-checks the blockers, so the listed impact may have changed.
      queryClient.invalidateQueries({ queryKey: impactKey })
    },
  })

  const impact = impactQuery.data
  const blockers = impact?.blockers ?? []
  const count = (value: number) => formatNumber(value, locale)

  return (
    <ConfirmDialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Delete organization?')}
      desc={t(
        'Deleting keeps the ledger, usage logs, and tasks as history. The organization is hidden from lists and cannot be used again.'
      )}
      confirmText={t('Delete organization')}
      destructive
      disabled={!impact || blockers.length > 0 || deleteMutation.isPending}
      isLoading={deleteMutation.isPending}
      handleConfirm={() => deleteMutation.mutate()}
    >
      <section className='flex flex-col gap-3 text-sm'>
        <h4 className='font-medium'>{t('Deletion impact')}</h4>
        {impact ? (
          <dl className='grid grid-cols-2 gap-x-4 gap-y-2'>
            <dt className='text-muted-foreground'>{t('Members')}</dt>
            <dd>{count(impact.member_count)}</dd>
            <dt className='text-muted-foreground'>{t('Admins')}</dt>
            <dd>{count(impact.admin_count)}</dd>
            <dt className='text-muted-foreground'>{t('Allocated quota')}</dt>
            <dd>{formatQuotaWithCurrency(impact.allocated_quota)}</dd>
            <dt className='text-muted-foreground'>{t('Used quota')}</dt>
            <dd>{formatQuotaWithCurrency(impact.used_quota)}</dd>
            <dt className='text-muted-foreground'>{t('Unallocated quota')}</dt>
            <dd>{formatQuotaWithCurrency(impact.unallocated_quota)}</dd>
            <dt className='text-muted-foreground'>
              {t('Enabled organization keys')}
            </dt>
            <dd>{count(impact.enabled_paid_keys)}</dd>
            <dt className='text-muted-foreground'>{t('Ledger entries')}</dt>
            <dd>{count(impact.ledger_entries)}</dd>
            <dt className='text-muted-foreground'>{t('Consume logs')}</dt>
            <dd>{count(impact.consume_logs)}</dd>
            <dt className='text-muted-foreground'>{t('Tasks')}</dt>
            <dd>{count(impact.tasks)}</dd>
          </dl>
        ) : null}
        {blockers.length > 0 ? (
          <div className='flex flex-col gap-1'>
            <h4 className='text-destructive font-medium'>
              {t('Blocking items')}
            </h4>
            <ul className='text-destructive list-disc pl-5'>
              {blockers.map((code) => (
                <li key={code}>
                  {BLOCKER_LABEL_KEYS[code]
                    ? t(BLOCKER_LABEL_KEYS[code])
                    : code}
                </li>
              ))}
            </ul>
          </div>
        ) : null}
      </section>
    </ConfirmDialog>
  )
}
