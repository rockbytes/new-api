import type { TFunction } from 'i18next'

import type { NameRule } from './types'

// ============================================================================
// Pagination
// ============================================================================

export const DEFAULT_PAGE_SIZE = 20

// ============================================================================
// Name Rule Options
// ============================================================================

export function getNameRuleOptions(t: TFunction) {
  return [
    { label: t('Exact Match'), value: 0 as NameRule },
    { label: t('Prefix Match'), value: 1 as NameRule },
    { label: t('Contains Match'), value: 2 as NameRule },
    { label: t('Suffix Match'), value: 3 as NameRule },
  ] as const
}

export function getNameRuleConfig(
  t: TFunction
): Record<NameRule, { label: string; color: string; description: string }> {
  return {
    0: {
      label: t('Exact'),
      color: 'green',
      description: t('Match model name exactly'),
    },
    1: {
      label: t('Prefix'),
      color: 'blue',
      description: t('Match models starting with this name'),
    },
    2: {
      label: t('Contains'),
      color: 'orange',
      description: t('Match models containing this name'),
    },
    3: {
      label: t('Suffix'),
      color: 'purple',
      description: t('Match models ending with this name'),
    },
  }
}

// ============================================================================
// Model Status
// ============================================================================

// ============================================================================
// Sync Status Options
// ============================================================================

// ============================================================================
// Deployment Status
// ============================================================================

export function getDeploymentStatusOptions(t: TFunction) {
  return [
    { label: t('All Status'), value: 'all' },
    { label: t('Running'), value: 'running' },
    { label: t('Completed'), value: 'completed' },
    { label: t('Failed'), value: 'failed' },
    { label: t('Deployment requested'), value: 'deployment requested' },
    { label: t('Termination requested'), value: 'termination requested' },
    { label: t('Destroyed'), value: 'destroyed' },
  ] as const
}

export function getDeploymentStatusConfig(t: TFunction): Record<
  string,
  {
    label: string
    variant: 'success' | 'neutral' | 'warning' | 'danger'
  }
> {
  return {
    running: { label: t('Running'), variant: 'success' },
    completed: { label: t('Completed'), variant: 'success' },
    failed: { label: t('Failed'), variant: 'danger' },
    error: { label: t('Failed'), variant: 'danger' },
    destroyed: { label: t('Destroyed'), variant: 'danger' },
    'deployment requested': {
      label: t('Deployment requested'),
      variant: 'warning',
    },
    'termination requested': {
      label: t('Termination requested'),
      variant: 'warning',
    },
  }
}

// ============================================================================
// Quota Type
// ============================================================================

// ============================================================================
// Endpoint Templates
// ============================================================================

export const ENDPOINT_TEMPLATES: Record<
  string,
  { path: string; method: string }
> = {
  openai: { path: '/v1/chat/completions', method: 'POST' },
  'openai-response': { path: '/v1/responses', method: 'POST' },
  anthropic: { path: '/v1/messages', method: 'POST' },
  gemini: { path: '/v1beta/models/{model}:generateContent', method: 'POST' },
  'jina-rerank': { path: '/rerank', method: 'POST' },
  'image-generation': { path: '/v1/images/generations', method: 'POST' },
  embeddings: { path: '/v1/embeddings', method: 'POST' },
}

// ============================================================================
// Sync Locale Options
// ============================================================================

export function getSyncLocaleOptions(t: TFunction) {
  return [
    { label: t('Chinese'), value: 'zh' },
    { label: t('English'), value: 'en' },
    { label: t('Japanese'), value: 'ja' },
  ] as const
}
