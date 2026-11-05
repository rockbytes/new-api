import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, test } from 'vitest'

import type { UsageLog } from '../../data/schema'
import type { LogOtherData } from '../../types'
import { DetailsDialog } from '../dialogs/details-dialog'

const queryClients: QueryClient[] = []

function makeErrorLog(other: LogOtherData): UsageLog {
  return {
    id: 2,
    user_id: 1,
    created_at: 1,
    type: 5,
    content: 'upstream failed',
    username: 'user',
    token_name: 'token',
    model_name: 'gpt-test',
    quota: 0,
    prompt_tokens: 0,
    completion_tokens: 0,
    use_time: 0,
    is_stream: false,
    channel: 1,
    channel_name: 'channel',
    token_id: 1,
    group: 'default',
    ip: '',
    other: JSON.stringify(other),
    request_id: 'req-2',
    upstream_request_id: '',
  }
}

function renderErrorDetails(isAdmin: boolean): void {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const freshAt = Date.now() + 60_000
  queryClient.setQueryData(['status'], {}, { updatedAt: freshAt })
  queryClients.push(queryClient)

  render(
    <QueryClientProvider client={queryClient}>
      <DetailsDialog
        log={makeErrorLog({
          admin_info: {
            error_source: 'upstream',
            error_stage: 'upstream_response',
            fault_side: 'upstream_link',
            fault_reason: 'upstream_reset',
            hop_trace: { upstream_error_kind: 'reset', upstream_error_ms: 303 },
          },
        })}
        isAdmin={isAdmin}
        isRoot={false}
        open
        onOpenChange={() => undefined}
      />
    </QueryClientProvider>
  )
}

afterEach(() => {
  for (const queryClient of queryClients) {
    queryClient.clear()
  }
  queryClients.length = 0
})

describe('usage log error attribution', () => {
  test('shows the error source and stage to administrators', () => {
    renderErrorDetails(true)

    expect(screen.getByText('Error source')).toBeInTheDocument()
    expect(screen.getByText('upstream')).toBeInTheDocument()
    expect(screen.getByText('Error stage')).toBeInTheDocument()
    expect(screen.getByText('upstream_response')).toBeInTheDocument()
    expect(screen.getByText('Link Attribution')).toBeInTheDocument()
    expect(screen.getByText('upstream_link')).toBeInTheDocument()
    expect(screen.getByText('upstream_reset')).toBeInTheDocument()
    expect(screen.getByText('upstream_error_ms')).toBeInTheDocument()
  })

  test('hides the error source and stage from non-admin users', () => {
    renderErrorDetails(false)

    expect(screen.queryByText('Error source')).toBeNull()
    expect(screen.queryByText('Error stage')).toBeNull()
    expect(screen.queryByText('upstream')).toBeNull()
    expect(screen.queryByText('upstream_response')).toBeNull()
    expect(screen.queryByText('Link Attribution')).toBeNull()
    expect(screen.queryByText('upstream_link')).toBeNull()
  })
})
