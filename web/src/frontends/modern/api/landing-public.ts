import type { ApiClient } from '@shared/http/client'

import { integer, list, record, text } from './response'

export interface LandingPublicStats {
  totalRequests: number
  totalTokens: number
}

export interface LandingPublicModel {
  name: string
  context: number | null
  prices: {
    input: string | null
    output: string | null
    cache_read: string | null
    cache_write: string | null
  }
}

export interface LandingPublicData {
  models: LandingPublicModel[]
  stats: LandingPublicStats
}

function nullableText(value: unknown): string | null {
  return value === null || value === undefined ? null : text(value)
}
function nullableInteger(value: unknown): number | null {
  return value === null || value === undefined ? null : integer(value)
}

export async function getLandingPublic(
  client: ApiClient,
  signal: AbortSignal,
): Promise<LandingPublicData> {
  const row = record(await client.request('/api/public/landing', { authKey: '', signal }))
  const stats = record(row.stats)
  return {
    models: list(row.models).map((item) => {
      const entry = record(item)
      const prices = record(entry.prices)
      return {
        name: text(entry.name),
        context: nullableInteger(entry.context),
        prices: {
          input: nullableText(prices.input),
          output: nullableText(prices.output),
          cache_read: nullableText(prices.cache_read),
          cache_write: nullableText(prices.cache_write),
        },
      }
    }),
    stats: {
      totalRequests: integer(stats.total_requests),
      totalTokens: integer(stats.total_tokens),
    },
  }
}
