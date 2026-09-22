import { reactive, readonly } from 'vue'

export interface UserProfile {
  id: number
  email: string
  status: string
  balance_nano_usd: number
  max_api_keys: number
  created_at_ms: number
}

interface StoredUserSession {
  token: string
  profile: UserProfile
  expiresAtMS: number
}

const userStorageKey = 'modern.userSession'

function readSession(storage?: Storage): StoredUserSession | null {
  try {
    const raw = storage?.getItem(userStorageKey)
    if (!raw) return null
    const value: unknown = JSON.parse(raw)
    if (typeof value !== 'object' || value === null) return null
    const session = value as Partial<StoredUserSession>
    if (
      typeof session.token !== 'string' ||
      session.token.length === 0 ||
      typeof session.profile !== 'object' ||
      session.profile === null ||
      typeof session.expiresAtMS !== 'number' ||
      !Number.isSafeInteger(session.expiresAtMS)
    ) {
      return null
    }
    return { token: session.token, profile: session.profile as UserProfile, expiresAtMS: session.expiresAtMS }
  } catch {
    return null
  }
}

function storeSession(storage: Storage | undefined, value: StoredUserSession | null): void {
  try {
    if (value) storage?.setItem(userStorageKey, JSON.stringify(value))
    else storage?.removeItem(userStorageKey)
  } catch {
    // 内存会话始终生效；存储不可用不能阻止登录或退出。
  }
}

export function createUserSession(deps: { localStorage?: Storage; sessionStorage?: Storage }) {
  const stored = readSession(deps.sessionStorage) ?? readSession(deps.localStorage)
  const state = reactive({
    token: stored?.token ?? '',
    profile: stored?.profile ?? null,
    expiresAtMS: stored?.expiresAtMS ?? 0,
  })

  function persist(): void {
    const value: StoredUserSession | null = state.token
      ? { token: state.token, profile: state.profile!, expiresAtMS: state.expiresAtMS }
      : null
    storeSession(deps.sessionStorage, value)
    storeSession(deps.localStorage, value)
  }

  return {
    state: readonly(state),
    getToken: () => state.token,
    getProfile: () => state.profile,
    hasSession: () => state.token.length > 0,
    isExpired: () => state.expiresAtMS > 0 && state.expiresAtMS <= Date.now(),
    setSession(token: string, profile: UserProfile, expiresAtMS: number): void {
      state.token = token
      state.profile = profile
      state.expiresAtMS = expiresAtMS
      persist()
    },
    clear(): void {
      state.token = ''
      state.profile = null
      state.expiresAtMS = 0
      persist()
    },
  }
}

export type UserSession = ReturnType<typeof createUserSession>

let session: UserSession | undefined

export function useUserSession(): UserSession {
  if (!session) {
    let storage: Storage | undefined
    try {
      storage = window.localStorage
    } catch {
      // 存储不可用时，会话仅在内存中维护。
    }
    session = createUserSession({ localStorage: storage })
  }
  return session
}
