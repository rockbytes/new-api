// ============================================================================
// Profile Constants
// ============================================================================

/**
 * Notification methods
 */
export const NOTIFICATION_METHODS = [
  { value: 'email' as const, label: 'Email' },
  { value: 'webhook' as const, label: 'Webhook' },
  { value: 'bark' as const, label: 'Bark' },
  { value: 'gotify' as const, label: 'Gotify' },
] as const

/**
 * Show the settings card (notifications, quota reminder, preferences) on the profile page. Hidden for now; set to true to restore it.
 */
export const PROFILE_SETTINGS_CARD_VISIBLE = false
