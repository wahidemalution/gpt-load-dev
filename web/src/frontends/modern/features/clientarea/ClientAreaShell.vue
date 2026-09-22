<script setup lang="ts">
import {
  ArrowLeft,
  ChartNoAxesCombined,
  House,
  KeyRound,
  Layers2,
  LogOut,
  Wallet,
} from '@lucide/vue'
import { computed, onMounted, onScopeDispose, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'

import { logout, me } from '@modern/api/clientarea'
import { formatNanoUSD } from '@modern/app/format'
import { AppButton, AppIcon, AppNotice } from '@modern/components/ui'
import { ApiError } from '@shared/http/errors'

import { provideClientAreaShell, useUserApiClient } from './client-area'
import { useUserSession, type UserProfile } from './user-session'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const client = useUserApiClient()
const session = useUserSession()

const profile = ref<UserProfile | null>(session.getProfile())
const loading = ref(false)
const failed = ref(false)
const loggingOut = ref(false)
let controller: AbortController | undefined

const anonymous = computed(() => session.state.token === '')
const balance = computed(() =>
  profile.value === null ? '—' : formatNanoUSD(profile.value.balance_nano_usd, locale.value),
)
const links = computed(() => [
  { name: 'modern-clientarea-dashboard', icon: House, label: t('clientarea.nav.overview') },
  { name: 'modern-clientarea-keys', icon: KeyRound, label: t('clientarea.nav.keys') },
  { name: 'modern-clientarea-usage', icon: ChartNoAxesCombined, label: t('clientarea.nav.usage') },
  { name: 'modern-clientarea-models', icon: Layers2, label: t('clientarea.nav.models') },
])

async function refresh(): Promise<void> {
  if (!session.hasSession()) return
  controller?.abort()
  const request = new AbortController()
  controller = request
  loading.value = true
  failed.value = false
  try {
    const next = await me(client, request.signal)
    if (!request.signal.aborted) profile.value = next
  } catch (cause) {
    if (request.signal.aborted) return
    // 401 clears the session (client-area.ts) -> shell falls back to inline 404.
    if (cause instanceof ApiError && cause.status === 401) return
    failed.value = true
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
}

provideClientAreaShell({ profile, refresh })
onMounted(refresh)
onScopeDispose(() => controller?.abort())
watch(() => route.fullPath, refresh)
watch(anonymous, (isAnonymous) => {
  if (isAnonymous) profile.value = null
})

async function signOut(): Promise<void> {
  if (loggingOut.value) return
  loggingOut.value = true
  try {
    await logout(client)
  } catch {
    // Best effort: the local session is cleared regardless.
  } finally {
    session.clear()
    loggingOut.value = false
    await router.replace({ name: 'modern-clientarea-home' })
  }
}
</script>

<template>
  <div class="modern-clientarea-shell">
    <aside v-if="!anonymous" class="modern-clientarea-sidebar">
      <RouterLink class="modern-clientarea-brand" :to="{ name: 'modern-clientarea-dashboard' }">
        Tokenizer
      </RouterLink>
      <nav class="modern-clientarea-nav" :aria-label="t('clientarea.nav.label')">
        <RouterLink
          v-for="link in links"
          :key="link.name"
          class="modern-clientarea-link"
          active-class="is-active"
          :to="{ name: link.name }"
        >
          <AppIcon :icon="link.icon" size="sm" />
          <span>{{ link.label }}</span>
        </RouterLink>
      </nav>
      <div class="modern-clientarea-account">
        <div class="modern-clientarea-balance">
          <span class="modern-clientarea-balance-label">
            <AppIcon :icon="Wallet" size="xs" />{{ t('clientarea.balance') }}
          </span>
          <span class="modern-clientarea-balance-value">{{ balance }}</span>
        </div>
        <AppButton
          variant="outline"
          size="sm"
          :icon="LogOut"
          :loading="loggingOut"
          @click="signOut"
        >
          {{ t('clientarea.logout') }}
        </AppButton>
      </div>
    </aside>

    <main class="modern-clientarea-main">
      <AppNotice v-if="failed && !anonymous" tone="warning">
        {{ t('clientarea.profileFailed') }}
        <template #actions>
          <AppButton size="xs" :loading="loading" @click="refresh">{{ t('ui.retry') }}</AppButton>
        </template>
      </AppNotice>
      <RouterView v-if="!anonymous" />
      <div v-else class="modern-clientarea-not-found">
        <p class="modern-clientarea-not-found-code">404</p>
        <p class="modern-clientarea-not-found-description">{{ t('notFound.description') }}</p>
        <div class="modern-clientarea-not-found-actions">
          <AppButton as-child>
            <RouterLink :to="{ name: 'modern-clientarea-home' }">
              <AppIcon :icon="ArrowLeft" size="sm" />{{ t('clientarea.auth.login') }}
            </RouterLink>
          </AppButton>
          <AppButton variant="outline" as-child>
            <RouterLink :to="{ name: 'modern-landing' }">
              {{ t('notFound.backHome') }}
            </RouterLink>
          </AppButton>
        </div>
      </div>
    </main>
  </div>
</template>

<style scoped>
.modern-clientarea-shell {
  display: grid;
  grid-template-columns: 240px minmax(0, 1fr);
  align-items: start;
  gap: var(--modern-space-6);
  width: min(100%, 1200px);
  min-height: calc(100dvh - var(--modern-topbar-height));
  padding: var(--modern-space-6) var(--modern-content-inset);
}
.modern-clientarea-sidebar {
  position: sticky;
  top: calc(var(--modern-topbar-height) + var(--modern-space-6));
  display: grid;
  gap: var(--modern-space-4);
  border: var(--modern-line-width) solid var(--modern-border);
  border-radius: var(--modern-radius-panel);
  background: var(--modern-surface);
  padding: var(--modern-space-4);
}
.modern-clientarea-brand {
  color: inherit;
  font-size: var(--modern-font-size-section);
  font-weight: var(--modern-weight-semibold);
  text-decoration: none;
}
.modern-clientarea-nav {
  display: grid;
  gap: var(--modern-space-1);
}
.modern-clientarea-link {
  display: flex;
  align-items: center;
  gap: var(--modern-space-2);
  border-radius: var(--modern-radius-control);
  padding: var(--modern-space-2) var(--modern-space-3);
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
  text-decoration: none;
}
.modern-clientarea-link:hover {
  background: var(--modern-subtle);
  color: var(--modern-text);
}
.modern-clientarea-link.is-active {
  background: var(--modern-accent-soft);
  color: var(--modern-accent);
  font-weight: var(--modern-weight-medium);
}
.modern-clientarea-account {
  display: grid;
  gap: var(--modern-space-3);
  border-top: var(--modern-line-width) solid var(--modern-border);
  padding-top: var(--modern-space-4);
}
.modern-clientarea-balance {
  display: grid;
  gap: var(--modern-space-1);
}
.modern-clientarea-balance-label {
  display: inline-flex;
  align-items: center;
  gap: var(--modern-space-1-5);
  color: var(--modern-muted);
  font-size: var(--modern-font-size-caption);
  font-weight: var(--modern-weight-medium);
  letter-spacing: var(--modern-tracking-label);
  text-transform: uppercase;
}
.modern-clientarea-balance-value {
  font-size: var(--modern-font-size-section);
  font-weight: var(--modern-weight-semibold);
  font-variant-numeric: tabular-nums;
}
.modern-clientarea-main {
  display: grid;
  gap: var(--modern-page-gap);
  min-width: 0;
  align-content: start;
}
.modern-clientarea-not-found {
  display: grid;
  justify-items: center;
  gap: var(--modern-space-4);
  padding: var(--modern-space-12) var(--modern-space-6);
  text-align: center;
}
.modern-clientarea-not-found-code {
  font-size: var(--modern-font-size-title);
  font-weight: var(--modern-weight-semibold);
}
.modern-clientarea-not-found-description {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-secondary);
}
.modern-clientarea-not-found-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: var(--modern-space-2);
}
.modern-clientarea-not-found-actions a {
  display: inline-flex;
  align-items: center;
  gap: var(--modern-space-1-5);
  color: inherit;
  text-decoration: none;
}
@media (max-width: 760px) {
  .modern-clientarea-shell {
    grid-template-columns: minmax(0, 1fr);
  }
  .modern-clientarea-sidebar {
    position: static;
  }
}
</style>
