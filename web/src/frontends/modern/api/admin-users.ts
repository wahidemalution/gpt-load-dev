import type { ApiClient } from '@shared/http/client'
import { InvalidResponseError } from '@shared/http/errors'
import { integer, list, record, text } from './response'

export interface AdminUserView {
  id: number
  email: string
  status: string
  balance_nano_usd: number
  max_api_keys: number
  key_count: number
  created_at_ms: number
  last_login_at_ms: number
}

export interface AdminUserKeyView {
  id: number
  name: string
  key_prefix: string
  key_suffix: string
  status: string
}

export interface AdminUserDetail extends AdminUserView {
  keys: AdminUserKeyView[]
}

export interface AdminUsersPage {
  items: AdminUserView[]
  total: number
}

export interface AdminListUsersParams {
  search?: string
  page?: number
  page_size?: number
}

export interface AdminCreateUserInput {
  email: string
  password?: string
  max_api_keys?: number
  balance_nano_usd?: number
}

export interface AdminCreateUserResult {
  profile: AdminUserView
  initial_password?: string
}

export interface AdminUpdateUserInput {
  status?: string
  max_api_keys?: number
}

export interface AdminAdjustmentInput {
  delta_nano_usd: number
  reason: string
  note?: string
}

export interface AdminAdjustmentView {
  id: number
  delta_nano_usd: number
  reason: string
  admin_note: string
  created_by_admin: string
  created_at_ms: number
}

export interface AdminAdjustmentsPage {
  items: AdminAdjustmentView[]
}

function readBalance(value: unknown): number {
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0)
    throw new InvalidResponseError()
  return value
}

function readStatus(value: unknown): string {
  const status = text(value)
  if (status !== 'active' && status !== 'disabled') throw new InvalidResponseError()
  return status
}

export function readAdminUserView(value: unknown): AdminUserView {
  const row = record(value)
  return {
    id: integer(row.id, 1),
    email: text(row.email),
    status: readStatus(row.status),
    balance_nano_usd: readBalance(row.balance_nano_usd),
    max_api_keys: integer(row.max_api_keys),
    key_count: integer(row.key_count),
    created_at_ms: integer(row.created_at_ms),
    last_login_at_ms: integer(row.last_login_at_ms),
  }
}

function readAdminUserKey(value: unknown): AdminUserKeyView {
  const row = record(value)
  return {
    id: integer(row.id, 1),
    name: text(row.name),
    key_prefix: text(row.key_prefix),
    key_suffix: text(row.key_suffix),
    status: text(row.status),
  }
}

export async function listUsers(
  client: ApiClient,
  params: AdminListUsersParams = {},
  signal?: AbortSignal,
): Promise<AdminUsersPage> {
  const query = new URLSearchParams()
  if (params.search) query.set('search', params.search)
  if (params.page !== undefined) query.set('page', String(params.page))
  if (params.page_size !== undefined) query.set('page_size', String(params.page_size))
  const suffix = query.size > 0 ? `?${query}` : ''
  const data = record(await client.request(`/api/admin/users${suffix}`, { ...(signal ? { signal } : {}) }))
  return {
    items: list(data.items).map(readAdminUserView),
    total: integer(data.total),
  }
}

export async function createUser(
  client: ApiClient,
  input: AdminCreateUserInput,
  signal?: AbortSignal,
): Promise<AdminCreateUserResult> {
  const row = record(
    await client.request('/api/admin/users', {
      method: 'POST',
      json: input,
      ...(signal ? { signal } : {}),
    }),
  )
  return {
    profile: readAdminUserView(row.profile),
    initial_password:
      row.initial_password === null || row.initial_password === undefined
        ? undefined
        : text(row.initial_password),
  }
}

export async function getUser(
  client: ApiClient,
  id: number,
  signal?: AbortSignal,
): Promise<AdminUserDetail> {
  const row = record(await client.request(`/api/admin/users/${id}`, { ...(signal ? { signal } : {}) }))
  const detail = readAdminUserView(row)
  return { ...detail, keys: list(row.keys).map(readAdminUserKey) }
}

export async function updateUser(
  client: ApiClient,
  id: number,
  patch: AdminUpdateUserInput,
  signal?: AbortSignal,
): Promise<AdminUserView> {
  return readAdminUserView(
    await client.request(`/api/admin/users/${id}`, {
      method: 'PATCH',
      json: patch,
      ...(signal ? { signal } : {}),
    }),
  )
}

function readAdjustment(value: unknown): AdminAdjustmentView {
  const row = record(value)
  const delta = row.delta_nano_usd
  if (typeof delta !== 'number' || !Number.isSafeInteger(delta)) throw new InvalidResponseError()
  return {
    id: integer(row.id, 1),
    delta_nano_usd: delta,
    reason: text(row.reason),
    admin_note: text(row.admin_note),
    created_by_admin: text(row.created_by_admin),
    created_at_ms: integer(row.created_at_ms),
  }
}

export async function listAdjustments(
  client: ApiClient,
  userID: number,
  params: { page?: number; page_size?: number } = {},
  signal?: AbortSignal,
): Promise<AdminAdjustmentsPage> {
  const query = new URLSearchParams()
  if (params.page !== undefined) query.set('page', String(params.page))
  if (params.page_size !== undefined) query.set('page_size', String(params.page_size))
  const suffix = query.size > 0 ? `?${query}` : ''
  return {
    items: list(
      await client.request(`/api/admin/users/${userID}/adjustments${suffix}`, {
        ...(signal ? { signal } : {}),
      }),
    ).map(readAdjustment),
  }
}

export async function createAdjustment(
  client: ApiClient,
  userID: number,
  input: AdminAdjustmentInput,
  signal?: AbortSignal,
): Promise<AdminUserView> {
  return readAdminUserView(
    await client.request(`/api/admin/users/${userID}/adjustments`, {
      method: 'POST',
      json: input,
      ...(signal ? { signal } : {}),
    }),
  )
}
