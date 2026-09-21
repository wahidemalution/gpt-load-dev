import type { ApiClient } from '@shared/http/client'

import { integer, list, record, text } from './response'

export interface LandingPublicStats {
  totalRequests: number
  totalTokens: number
}

export interface LandingPublicData {
  models: string[]
  stats: LandingPublicStats
}

export async function getLandingPublic(
  client: ApiClient,
  signal: AbortSignal,
): Promise<LandingPublicData> {
  const row = record(await client.request('/api/public/landing', { authKey: '', signal }))
  const stats = record(row.stats)
  return {
    models: list(row.models).map((item) => text(record(item).name)),
    stats: {
      totalRequests: integer(stats.total_requests),
      totalTokens: integer(stats.total_tokens),
    },
  }
}
