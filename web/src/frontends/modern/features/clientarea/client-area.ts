import { inject, provide, type InjectionKey, type Ref } from 'vue'

import type { ApiClient, ApiPath, ApiRequestOptions } from '@shared/http/client'
import { useApiClient } from '@shared/http/client-context'
import { ApiError } from '@shared/http/errors'

import { useUserSession, type UserProfile } from './user-session'

// Wraps the injected admin-scoped client so client-area calls carry the user
// token instead of the admin credential. `handleUnauthorized: false` keeps a
// user-token 401 from clearing the admin session; we clear the user session.
export function useUserApiClient(): ApiClient {
  const client = useApiClient()
  const session = useUserSession()
  async function request<T>(path: ApiPath, options?: ApiRequestOptions): Promise<T> {
    try {
      return await client.request<T>(path, {
        ...options,
        authKey: session.getToken(),
        handleUnauthorized: false,
      })
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) session.clear()
      throw error
    }
  }
  return { request }
}

export interface ClientAreaShellContext {
  profile: Ref<UserProfile | null>
  refresh(): Promise<void>
}

const clientAreaShellKey: InjectionKey<ClientAreaShellContext> = Symbol('client-area-shell')

export function provideClientAreaShell(context: ClientAreaShellContext): void {
  provide(clientAreaShellKey, context)
}

export function useClientAreaShell(): ClientAreaShellContext {
  const context = inject(clientAreaShellKey, null)
  if (!context) throw new Error('CLIENT_AREA_SHELL_NOT_PROVIDED')
  return context
}
