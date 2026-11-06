import { useTranslation } from 'react-i18next'

import { useStatus } from '@/hooks/use-status'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

// The running version, shown to administrators in the header. It only displays the version: it
// neither opens a dialog nor checks for releases. Update checks live in the maintenance settings.
export function SystemVersion() {
  const { t } = useTranslation()
  const isAdmin = useAuthStore(
    (state) => (state.auth.user?.role ?? 0) >= ROLE.ADMIN
  )
  const { status } = useStatus()
  if (!isAdmin) return null

  const raw = typeof status?.version === 'string' ? status.version.trim() : ''
  const isUnknown = raw === '' || raw === 'v0.0.0' || raw === '0.0.0'
  const version = isUnknown ? t('Unknown version') : raw

  return (
    <span
      className='text-muted-foreground hidden max-w-32 truncate font-mono text-xs @min-[22rem]/system-brand:inline'
      title={version}
    >
      {version}
    </span>
  )
}
