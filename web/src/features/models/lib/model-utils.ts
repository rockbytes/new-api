import type { Model } from '../types'

// ============================================================================
// Time Formatting
// ============================================================================

// ============================================================================
// Tags Parsing
// ============================================================================

/**
 * Parse tags string to array
 */
export function parseModelTags(tags: string | undefined): string[] {
  if (!tags) return []
  return tags
    .split(',')
    .map((tag) => tag.trim())
    .filter(Boolean)
}

// ============================================================================
// Endpoints Parsing
// ============================================================================

/**
 * Parse endpoints JSON string
 */
function parseEndpoints(
  endpoints: string | undefined
): Record<string, unknown> | unknown[] | null {
  if (!endpoints || endpoints.trim() === '') return null

  try {
    return JSON.parse(endpoints)
  } catch {
    return null
  }
}

/**
 * Format endpoints to display
 */
export function formatEndpointsDisplay(
  endpoints: string | undefined
): string[] {
  const parsed = parseEndpoints(endpoints)
  if (!parsed) return []

  if (typeof parsed === 'object' && !Array.isArray(parsed)) {
    return Object.keys(parsed)
  }

  if (Array.isArray(parsed)) {
    return parsed.map(String)
  }

  return []
}

// ============================================================================
// Name Rule Utils
// ============================================================================

// ============================================================================
// Quota Type Utils
// ============================================================================

// ============================================================================
// Model Validation
// ============================================================================

// ============================================================================
// Model Status Utils
// ============================================================================

/**
 * Check if model is enabled
 */
export function isModelEnabled(model: Model): boolean {
  return model.status === 1
}

// Keep table labels compact; the drawer and tooltip share the full explanation.
export function getModelChannelState(model: Model) {
  const available = model.bound_channels?.length ?? 0
  const configured = model.configured_channel_count ?? available
  if (configured === 0) {
    if (model.name_rule !== 0) {
      return {
        label: 'No matching channels',
        description: 'No configured channel models match this metadata rule.',
      }
    }
    return {
      label: 'Metadata only',
      description:
        'No channel is configured. This model will not appear in the model square.',
    }
  }
  if (available === 0) {
    return {
      label: 'No available channels',
      description:
        'No channel is currently available. This model will not appear in the model square.',
    }
  }
  return {
    label: 'Available channels: {{count}}',
    description:
      'Listing also depends on metadata visibility and the user’s group access.',
  }
}
