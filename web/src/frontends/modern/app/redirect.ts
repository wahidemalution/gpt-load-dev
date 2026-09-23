import type { Router } from 'vue-router'
import { pagePath } from './navigation'

const sessionKey = 'modern.postAuthPath'

export function loginLocation() {
  return { name: 'modern-login' }
}

export function rememberPostAuthPath(fullPath: string): void {
  try {
    sessionStorage.setItem(sessionKey, fullPath)
  } catch {
    // sessionStorage unavailable (private mode) — skip silently
  }
}

export function takePostAuthPath(): string | null {
  try {
    const value = sessionStorage.getItem(sessionKey)
    sessionStorage.removeItem(sessionKey)
    return value
  } catch {
    return null
  }
}

export function safeRedirect(raw: unknown, router: Router): string {
  const fallback = pagePath('home')
  if (
    typeof raw !== 'string' ||
    !raw.startsWith('/') ||
    raw.startsWith('//') ||
    raw.includes('\\')
  ) {
    return fallback
  }
  let decoded: string
  try {
    decoded = decodeURIComponent(raw)
  } catch {
    return fallback
  }
  if (
    decoded.startsWith('//') ||
    decoded.includes('\\') ||
    decoded.split('').some((char) => {
      const code = char.codePointAt(0) ?? 0
      return code < 0x20 || code === 0x7f
    })
  ) {
    return fallback
  }
  const resolved = router.resolve(raw)
  if (resolved.name === 'modern-unavailable') return '/404'
  const matchedPath = resolved.matched.at(-1)?.path
  let segments: string[]
  try {
    segments = decodeURIComponent(resolved.path).split('/')
  } catch {
    return fallback
  }
  const pattern = matchedPath?.split('/') ?? []
  if (
    !resolved.matched.length ||
    segments.length !== pattern.length ||
    !pattern.every((segment, index) =>
      segment.startsWith(':') ? segments[index] !== '' : segment === segments[index],
    ) ||
    resolved.name === 'modern-login' ||
    resolved.meta.requiresAuth !== true
  )
    return fallback
  return resolved.fullPath
}
