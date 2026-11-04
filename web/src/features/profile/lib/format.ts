import type { UserProfile, UserSettings } from '../types'

// ============================================================================
// Profile Formatting Utilities
// ============================================================================

/**
 * Parse user settings from JSON string
 */
export function parseUserSettings(settingsJson?: string): UserSettings {
  if (!settingsJson) return {}

  try {
    return JSON.parse(settingsJson) as UserSettings
  } catch {
    return {}
  }
}

/**
 * Get display name or fallback to username
 */
export function getDisplayName(user?: UserProfile): string {
  if (!user) return ''
  return user.display_name || user.username
}

