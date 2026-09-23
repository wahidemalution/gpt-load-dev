import type { ApiClient } from '@shared/http/client'
import { InvalidResponseError } from '@shared/http/errors'
import { integer, list, record, signedInteger, text } from './response'
import type { UserProfile } from '@modern/features/clientarea/user-session'

export interface ClientAreaClient {
  request: ApiClient['request']
}

function readProfile(value: unknown): UserProfile {
  const row = record(value)
  const status = text(row.status)
  if (status !== 'active' && status !== 'disabled') throw new InvalidResponseError()
  return {
    id: integer(row.id, 1),
    email: text(row.email),
    status,
    balance_nano_usd: signedInteger(row.balance_nano_usd),
    max_api_keys: integer(row.max_api_keys),
    created_at_ms: integer(row.created_at_ms),
  }
}

export interface OwnedKeyView {
  id: number
  name: string
  key_prefix: string
  key_suffix: string
  status: string
  created_at_ms: number
  last_request_at_ms: number | null
  rpm_limit: number
  max_concurrent: number
  credit_limit_nano_usd: number
  models: string[]
  usage_total_nano_usd: number
}

export interface KeyLimitsInput {
  rpm_limit?: number
  max_concurrent?: number
  credit_limit_nano_usd?: number
  models?: string[]
}

export interface ClientAreaModel {
  name: string
  context: number | null
  prices: {
    input: string | null
    output: string | null
    cache_read: string | null
    cache_write: string | null
  }
}

export interface ClientAreaUsageRow {
  model: string
  request_count: number
  estimated_cost_nano_usd: number
}

export interface ClientAreaDailyTokenRow {
  day_ms: number
  total_tokens: number
}

export interface ClientAreaUsageResult {
  models: ClientAreaUsageRow[]
  daily: ClientAreaDailyTokenRow[]
}

export interface ClientAreaUsageParams {
  from_ms: number
  to_ms: number
  granularity?: 'day'
}

function readOwnedKey(value: unknown): OwnedKeyView {
  const row = record(value)
  return {
    id: integer(row.id, 1),
    name: text(row.name),
    key_prefix: text(row.key_prefix),
    key_suffix: text(row.key_suffix),
    status: text(row.status),
    created_at_ms: integer(row.created_at_ms),
    last_request_at_ms:
      row.last_request_at_ms === null || row.last_request_at_ms === undefined
        ? null
        : integer(row.last_request_at_ms),
    rpm_limit: integer(row.rpm_limit),
    max_concurrent: integer(row.max_concurrent),
    credit_limit_nano_usd: integer(row.credit_limit_nano_usd),
    models: list(row.models).map((value) => text(value)),
    usage_total_nano_usd: integer(row.usage_total_nano_usd),
  }
}

function readModel(value: unknown): ClientAreaModel {
  const row = record(value)
  const prices = record(row.prices)
  return {
    name: text(row.name),
    context: row.context === null || row.context === undefined ? null : integer(row.context),
    prices: {
      input: prices.input === null || prices.input === undefined ? null : text(prices.input),
      output: prices.output === null || prices.output === undefined ? null : text(prices.output),
      cache_read:
        prices.cache_read === null || prices.cache_read === undefined
          ? null
          : text(prices.cache_read),
      cache_write:
        prices.cache_write === null || prices.cache_write === undefined
          ? null
          : text(prices.cache_write),
    },
  }
}

function readUsageRow(value: unknown): ClientAreaUsageRow {
  const row = record(value)
  return {
    model: text(row.model),
    request_count: integer(row.request_count),
    estimated_cost_nano_usd: integer(row.estimated_cost_nano_usd),
  }
}

function readDailyTokenRow(value: unknown): ClientAreaDailyTokenRow {
  const row = record(value)
  return {
    day_ms: integer(row.day_ms),
    total_tokens: integer(row.total_tokens),
  }
}

export interface ClientAreaLoginResult {
  token: string
  profile: UserProfile
  expires_at_ms: number
}

export interface ClientAreaCreateKeyResult {
  key: string
  profile: OwnedKeyView
}

export async function signup(
  client: ApiClient,
  email: string,
  password: string,
  signal?: AbortSignal,
): Promise<UserProfile> {
  return readProfile(
    await client.request('/api/clientarea/signup', {
      method: 'POST',
      json: { email, password },
      handleUnauthorized: false,
      ...(signal ? { signal } : {}),
    }),
  )
}

export async function login(
  client: ApiClient,
  email: string,
  password: string,
  signal?: AbortSignal,
): Promise<ClientAreaLoginResult> {
  const row = record(
    await client.request('/api/clientarea/login', {
      method: 'POST',
      json: { email, password },
      handleUnauthorized: false,
      ...(signal ? { signal } : {}),
    }),
  )
  const token = text(row.token)
  if (token.length === 0) throw new InvalidResponseError()
  const expiresAtMS = integer(row.expires_at_ms)
  if (expiresAtMS <= 0) throw new InvalidResponseError()
  return { token, profile: readProfile(row.profile), expires_at_ms: expiresAtMS }
}

export async function logout(client: ApiClient, signal?: AbortSignal): Promise<void> {
  try {
    await client.request('/api/clientarea/logout', {
      method: 'POST',
      ...(signal ? { signal } : {}),
    })
  } catch (error) {
    // 服务端契约是 204 无响应体，共享 envelope 解析器对空响应体抛错；2xx 即登出成功。
    if (error instanceof InvalidResponseError) return
    throw error
  }
}

export async function me(client: ApiClient, signal?: AbortSignal): Promise<UserProfile> {
  return readProfile(await client.request('/api/clientarea/me', { ...(signal ? { signal } : {}) }))
}

export async function listModels(
  client: ApiClient,
  signal?: AbortSignal,
): Promise<ClientAreaModel[]> {
  return list(
    await client.request('/api/clientarea/models', { ...(signal ? { signal } : {}) }),
  ).map(readModel)
}

export async function listKeys(client: ApiClient, signal?: AbortSignal): Promise<OwnedKeyView[]> {
  return list(await client.request('/api/clientarea/keys', { ...(signal ? { signal } : {}) })).map(
    readOwnedKey,
  )
}

export async function createKey(
  client: ApiClient,
  name: string,
  signal?: AbortSignal,
): Promise<ClientAreaCreateKeyResult> {
  const row = record(
    await client.request('/api/clientarea/keys', {
      method: 'POST',
      json: { name },
      ...(signal ? { signal } : {}),
    }),
  )
  const key = text(row.key)
  if (key.length === 0) throw new InvalidResponseError()
  return { key, profile: readOwnedKey(row.profile) }
}

export async function deleteKey(
  client: ApiClient,
  id: number,
  signal?: AbortSignal,
): Promise<void> {
  try {
    await client.request(`/api/clientarea/keys/${id}`, {
      method: 'DELETE',
      ...(signal ? { signal } : {}),
    })
  } catch (error) {
    // 服务端契约是 200 空响应体；envelope 解析器对空响应体抛错，2xx 即删除成功。
    if (error instanceof InvalidResponseError) return
    throw error
  }
}

export async function updateKeyLimits(
  client: ApiClient,
  id: number,
  input: KeyLimitsInput,
  signal?: AbortSignal,
): Promise<void> {
  try {
    await client.request(`/api/clientarea/keys/${id}/limits`, {
      method: 'PATCH',
      json: input,
      ...(signal ? { signal } : {}),
    })
  } catch (error) {
    // 服务端契约是 200 空响应体；envelope 解析器对空响应体抛错，2xx 即更新成功。
    if (error instanceof InvalidResponseError) return
    throw error
  }
}

export async function updateKeyStatus(
  client: ApiClient,
  id: number,
  status: 'active' | 'disabled',
  signal?: AbortSignal,
): Promise<void> {
  try {
    await client.request(`/api/clientarea/keys/${id}/status`, {
      method: 'PATCH',
      json: { status },
      ...(signal ? { signal } : {}),
    })
  } catch (error) {
    // 服务端契约是 200 空响应体；envelope 解析器对空响应体抛错，2xx 即更新成功。
    if (error instanceof InvalidResponseError) return
    throw error
  }
}

export async function usage(
  client: ApiClient,
  params: ClientAreaUsageParams,
  signal?: AbortSignal,
): Promise<ClientAreaUsageRow[]> {
  const query = new URLSearchParams({
    from_ms: String(params.from_ms),
    to_ms: String(params.to_ms),
  })
  return list(
    await client.request(`/api/clientarea/usage?${query}`, { ...(signal ? { signal } : {}) }),
  ).map(readUsageRow)
}

export async function usageWithDaily(
  client: ApiClient,
  params: ClientAreaUsageParams,
  signal?: AbortSignal,
): Promise<ClientAreaUsageResult> {
  const query = new URLSearchParams({
    from_ms: String(params.from_ms),
    to_ms: String(params.to_ms),
    granularity: params.granularity ?? 'day',
  })
  const row = record(
    await client.request(`/api/clientarea/usage?${query}`, { ...(signal ? { signal } : {}) }),
  )
  return {
    models: list(row.models).map(readUsageRow),
    daily: list(row.daily).map(readDailyTokenRow),
  }
}
