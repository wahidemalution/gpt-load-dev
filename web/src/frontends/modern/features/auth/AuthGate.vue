<script setup lang="ts">
import { ArrowLeft, LoaderCircle } from '@lucide/vue'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { isNavigationFailure, RouterLink, useRoute, useRouter } from 'vue-router'

import { AppButton, AppIcon, AppNotice } from '@modern/components/ui'
import PublicLayout from '@modern/layouts/PublicLayout.vue'
import LandingLayout from '@modern/layouts/LandingLayout.vue'
import { rememberPostAuthPath } from '@modern/app/redirect'
import AuthCard from './AuthCard.vue'
import { useAuthSession } from './auth-session'
import { useCountdown } from './use-countdown'

const session = useAuthSession()
const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const countdown = useCountdown()
const actions = ref<HTMLElement>()
const navigationFailed = ref(false)
const canRender = computed(
  () =>
    session.state.phase === 'validated' &&
    (!route.meta.adminOnly || session.state.principalType === 'admin'),
)
const message = computed(() => {
  if (navigationFailed.value) return t('shell.navigationFailed')
  if (session.state.phase === 'locked')
    return countdown.active.value
      ? t('auth.locked', { seconds: countdown.seconds.value })
      : t('auth.lockEnded')
  if (session.state.phase === 'network-error') return t('auth.network')
  if (session.state.phase === 'invalid-response') return t('auth.invalidResponse')
  return ''
})

async function navigate(target: { name: string }): Promise<void> {
  try {
    const failure = await router.replace(target)
    navigationFailed.value = isNavigationFailure(failure)
  } catch {
    navigationFailed.value = true
  }
}

watch(
  [() => session.state.phase, () => session.state.principalType, () => route.fullPath],
  ([phase, principal]) => {
    if (phase === 'unvalidated') void session.ensureValidated()
    if (phase === 'anonymous') {
      // ponytail: jangan redirect ke /login — non-user tidak boleh tahu path
      // dashboard mana yang valid; cukup render halaman 404 di tempat
      navigationFailed.value = false
    }
    if (phase === 'validated' && principal === 'access_key' && route.meta.adminOnly) {
      void navigate({ name: 'modern-home' })
    }
  },
  { immediate: true },
)
watch(
  [() => session.state.phase, () => session.state.retryAfterSeconds],
  ([phase, seconds]) => {
    if (phase === 'locked') countdown.start(seconds)
  },
  { immediate: true },
)
watch(
  () => session.state.phase,
  async (phase) => {
    if (phase !== 'network-error' && phase !== 'invalid-response') return
    await nextTick()
    actions.value?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus()
  },
)

function retry(): void {
  if (countdown.active.value && session.state.phase === 'locked') return
  void session.ensureValidated()
}
function reload(): void {
  window.location.reload()
}
</script>

<template>
  <slot v-if="canRender" />
  <LandingLayout v-else-if="session.state.phase === 'anonymous'">
    <div class="modern-auth-gate-not-found">
      <p class="modern-auth-gate-not-found-code">404</p>
      <p class="modern-auth-gate-not-found-description">{{ t('notFound.description') }}</p>
      <div class="modern-auth-gate-not-found-actions">
        <AppButton as-child>
          <RouterLink :to="{ name: 'modern-landing' }">
            <AppIcon :icon="ArrowLeft" size="sm" />{{ t('notFound.backHome') }}
          </RouterLink>
        </AppButton>
        <AppButton variant="outline" as-child>
          <RouterLink :to="{ name: 'modern-login' }" @click="rememberPostAuthPath(route.fullPath)">
            {{ t('auth.title') }}
          </RouterLink>
        </AppButton>
      </div>
    </div>
  </LandingLayout>
  <PublicLayout v-else restoring-session>
    <AuthCard :title="t('auth.restoreTitle')">
      <template v-if="message">
        <AppNotice :tone="session.state.phase === 'locked' ? 'warning' : 'danger'">{{
          message
        }}</AppNotice>
        <div ref="actions" class="modern-auth-gate-actions">
          <AppButton v-if="navigationFailed" @click="reload">{{ t('shell.reload') }}</AppButton>
          <AppButton
            v-else
            :disabled="session.state.phase === 'locked' && countdown.active.value"
            @click="retry"
            >{{ t('auth.retry') }}</AppButton
          >
          <AppButton variant="ghost" @click="session.clear()">{{ t('auth.changeKey') }}</AppButton>
        </div>
      </template>
      <div v-else class="modern-auth-checking" role="status">
        <AppIcon :icon="LoaderCircle" class="modern-spin" />
        <span>{{ t('auth.checking') }}</span>
      </div>
    </AuthCard>
  </PublicLayout>
</template>

<style scoped>
.modern-auth-checking,
.modern-auth-gate-actions {
  display: flex;
  align-items: center;
  gap: var(--modern-space-2);
}
.modern-auth-checking {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-secondary);
}
.modern-auth-gate-actions {
  flex-wrap: wrap;
}
.modern-auth-gate-not-found {
  display: grid;
  justify-items: center;
  gap: var(--modern-space-4);
  padding: var(--modern-space-12) var(--modern-content-inset);
  text-align: center;
}
.modern-auth-gate-not-found-code {
  font-size: var(--modern-font-size-title);
  font-weight: var(--modern-weight-semibold);
}
.modern-auth-gate-not-found-description {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-secondary);
}
.modern-auth-gate-not-found a {
  display: inline-flex;
  align-items: center;
  gap: var(--modern-space-1-5);
  color: inherit;
  text-decoration: none;
}
.modern-auth-gate-not-found-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: var(--modern-space-2);
}
</style>
