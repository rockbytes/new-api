import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

export interface ChangeOwnPasswordResult {
  relogin_required: boolean
}

export async function changeOwnPassword(
  oldPassword: string,
  newPassword: string
): Promise<ChangeOwnPasswordResult> {
  const res = await api.put('/api/user/self/password', {
    old_password: oldPassword,
    new_password: newPassword,
  })
  return requireServerSuccess(res.data).data
}
