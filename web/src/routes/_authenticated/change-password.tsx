import { createFileRoute } from '@tanstack/react-router'

import { PasswordChange } from '@/features/password-change'

export const Route = createFileRoute('/_authenticated/change-password')({
  component: PasswordChange,
})
