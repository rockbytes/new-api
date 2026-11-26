import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowRight, CircleAlert, CircleCheck } from 'lucide-react'
import { useId } from 'react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { buttonVariants } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { getOverviewAccountStatus } from '@/features/dashboard/api'
import { useIsSidebarModuleVisible } from '@/hooks/use-sidebar-config'
import { formatQuota } from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'
import { useSystemConfigStore } from '@/stores/system-config-store'

const ACCOUNT_STATUS_REFRESH_MS = 60_000

interface AccountMetricsProps {
  balanceLabel: string
  balance: string
  usage: string
}

function AccountMetrics(props: AccountMetricsProps) {
  const { t } = useTranslation()

  return (
    <dl className='grid grid-cols-2 gap-3'>
      <div className='min-w-0'>
        <dt className='text-muted-foreground text-xs'>{props.balanceLabel}</dt>
        <dd className='mt-1 text-xl leading-snug font-semibold [overflow-wrap:anywhere] tabular-nums'>
          {props.balance}
        </dd>
      </div>
      <div className='min-w-0'>
        <dt className='text-muted-foreground text-xs'>{t('Last 24h usage')}</dt>
        <dd className='mt-1 text-xl leading-snug font-semibold [overflow-wrap:anywhere] tabular-nums'>
          {props.usage}
        </dd>
      </div>
    </dl>
  )
}

export function AccountStatusCard() {
  const { t } = useTranslation()
  const titleId = useId()
  const personalId = `${titleId}-personal`
  const organizationId = `${titleId}-organization`
  const userId = useAuthStore((state) => state.auth.user?.id)
  const sessionId = useAuthStore((state) => state.auth.session?.sid)
  const remaining = useAuthStore((state) => state.auth.user?.quota)
  // The quota formatter reads the current display configuration from this store.
  useSystemConfigStore((state) => state.config.currency)
  const showUsageReport = useIsSidebarModuleVisible('/usage-report')
  // Membership and both organization figures are read together. There is no
  // separate organization balance/usage cache that could survive a switch.
  const accountQuery = useQuery({
    queryKey: ['dashboard', 'overview', 'account-status', userId, sessionId],
    queryFn: ({ signal }) => getOverviewAccountStatus(signal),
    enabled: userId != null,
    refetchOnMount: 'always',
    refetchOnWindowFocus: 'always',
    refetchInterval: ACCOUNT_STATUS_REFRESH_MS,
    meta: { errorToast: false },
  })
  const fallback = accountQuery.isPending ? t('Loading...') : '-'
  const account = accountQuery.isSuccess ? accountQuery.data : undefined
  // Hide cached organization data while revalidating on mount or focus.
  const revalidatingOrganization = accountQuery.isFetching
  const organization = revalidatingOrganization
    ? undefined
    : account?.organization
  const noOrganization =
    organization === null && !account?.organization_unavailable
  const organizationFallback = revalidatingOrganization
    ? t('Loading...')
    : fallback
  const balanceDepleted = remaining != null && remaining <= 0

  return (
    <Card role='region' aria-labelledby={titleId} className='gap-3'>
      <CardHeader className='flex flex-wrap items-center justify-between gap-2'>
        <CardTitle>
          <h3 id={titleId}>{t('Account status')}</h3>
        </CardTitle>
        {showUsageReport && (
          <Link
            to='/usage-report'
            className={buttonVariants({ size: 'sm', variant: 'outline' })}
          >
            {t('View usage report')}
            <ArrowRight data-icon='inline-end' aria-hidden='true' />
          </Link>
        )}
      </CardHeader>
      <CardContent className='grid grid-cols-1 gap-3 md:grid-cols-2'>
        <section
          aria-labelledby={personalId}
          className='min-w-0 rounded-lg border p-3'
        >
          <div className='mb-3 flex flex-wrap items-center gap-2'>
            <h4 id={personalId} className='font-medium'>
              {t('Personal')}
            </h4>
            {remaining != null && (
              <StatusBadge
                variant={balanceDepleted ? 'danger' : 'success'}
                icon={balanceDepleted ? CircleAlert : CircleCheck}
                label={balanceDepleted ? t('Balance depleted') : t('Healthy')}
                copyable={false}
              />
            )}
          </div>
          <AccountMetrics
            balanceLabel={t('Personal balance')}
            balance={remaining == null ? '-' : formatQuota(remaining)}
            usage={
              account?.personal_usage == null
                ? fallback
                : formatQuota(account.personal_usage)
            }
          />
        </section>
        <section
          aria-labelledby={organizationId}
          className='min-w-0 rounded-lg border p-3'
        >
          <div className='mb-3 flex flex-wrap items-center gap-x-2 gap-y-1'>
            <h4 id={organizationId} className='font-medium'>
              {t('Organization')}
            </h4>
            {(organization || noOrganization) && (
              <span className='text-muted-foreground min-w-0 text-xs [overflow-wrap:anywhere]'>
                {organization?.name ?? t('No organization')}
              </span>
            )}
          </div>
          <AccountMetrics
            balanceLabel={t('My remaining allocation')}
            balance={
              organization?.remaining_quota == null
                ? organizationFallback
                : formatQuota(organization.remaining_quota)
            }
            usage={
              organization?.recent_usage == null
                ? organizationFallback
                : formatQuota(organization.recent_usage)
            }
          />
        </section>
      </CardContent>
    </Card>
  )
}
