<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { login, signup } from '@modern/api/clientarea'
import { AppButton, AppNotice, AppSegmentedControl, AppTextField } from '@modern/components/ui'
import AuthCard from '@modern/features/auth/AuthCard.vue'
import { ApiError, NetworkError } from '@shared/http/errors'

import { useUserApiClient } from './client-area'
import { useUserSession } from './user-session'

type Mode = 'login' | 'signup'

const { t } = useI18n()
const router = useRouter()
const client = useUserApiClient()
const session = useUserSession()

const mode = ref<Mode>('login')
const email = ref('')
const password = ref('')
const confirm = ref('')
const busy = ref(false)
const error = ref('')
const confirmError = ref('')

const tabs = computed(() => [
  { value: 'login', label: t('clientarea.auth.login') },
  { value: 'signup', label: t('clientarea.auth.signup') },
])

onMounted(() => {
  if (session.hasSession() && !session.isExpired()) {
    void router.replace({ name: 'modern-clientarea-dashboard' })
    return
  }
  // An expired token would only bounce off the dashboard's 401; drop it here.
  if (session.hasSession()) session.clear()
})

function switchMode(value: string): void {
  if (value !== 'login' && value !== 'signup') return
  mode.value = value
  error.value = ''
  confirmError.value = ''
}

function messageFor(cause: unknown): string {
  if (cause instanceof ApiError) {
    if (cause.status === 409) return t('clientarea.auth.errors.emailTaken')
    if (cause.status === 401) return t('clientarea.auth.errors.wrongCredentials')
    if (cause.status === 429) return t('clientarea.auth.errors.tooManyAttempts')
  }
  if (cause instanceof NetworkError) return t('clientarea.auth.errors.network')
  return t('clientarea.auth.failed')
}

async function submit(): Promise<void> {
  if (busy.value) return
  confirmError.value = ''
  if (mode.value === 'signup' && password.value !== confirm.value) {
    confirmError.value = t('clientarea.auth.passwordMismatch')
    return
  }
  busy.value = true
  error.value = ''
  try {
    const address = email.value.trim()
    if (mode.value === 'signup') await signup(client, address, password.value)
    const result = await login(client, address, password.value)
    session.setSession(result.token, result.profile, result.expires_at_ms)
    await router.replace({ name: 'modern-clientarea-dashboard' })
  } catch (cause) {
    error.value = messageFor(cause)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="modern-clientarea-auth">
    <AuthCard :title="t('clientarea.auth.title')" :description="t('clientarea.auth.description')">
      <AppSegmentedControl
        :model-value="mode"
        :options="tabs"
        :label="t('clientarea.auth.title')"
        appearance="field"
        @update:model-value="switchMode"
      />
      <form class="modern-clientarea-auth-form" novalidate @submit.prevent="submit">
        <AppTextField
          v-model="email"
          :label="t('clientarea.auth.email')"
          name="email"
          type="email"
          autocomplete="email"
          autocapitalize="none"
          :spellcheck="false"
          :disabled="busy"
        />
        <AppTextField
          v-model="password"
          :label="t('clientarea.auth.password')"
          name="password"
          type="password"
          :autocomplete="mode === 'signup' ? 'new-password' : 'current-password'"
          :disabled="busy"
        />
        <AppTextField
          v-if="mode === 'signup'"
          v-model="confirm"
          :label="t('clientarea.auth.confirmPassword')"
          name="password-confirm"
          type="password"
          autocomplete="new-password"
          :disabled="busy"
          :error="confirmError || undefined"
        />
        <AppNotice v-if="error" tone="danger">{{ error }}</AppNotice>
        <AppButton
          class="modern-clientarea-auth-submit"
          variant="primary"
          type="submit"
          :loading="busy"
        >
          {{ t(mode === 'signup' ? 'clientarea.auth.signup' : 'clientarea.auth.login') }}
        </AppButton>
      </form>
      <AppButton
        class="modern-clientarea-auth-switch"
        variant="text"
        size="sm"
        @click="switchMode(mode === 'signup' ? 'login' : 'signup')"
      >
        {{
          t(mode === 'signup' ? 'clientarea.auth.switchToLogin' : 'clientarea.auth.switchToSignup')
        }}
      </AppButton>
    </AuthCard>
  </div>
</template>

<style scoped>
.modern-clientarea-auth {
  display: grid;
  justify-items: center;
  width: min(100%, 480px);
  padding: var(--modern-space-12) var(--modern-content-inset);
}
.modern-clientarea-auth-form {
  display: grid;
  gap: var(--modern-space-4);
}
.modern-clientarea-auth-submit {
  width: 100%;
}
.modern-clientarea-auth-switch {
  align-self: center;
}
</style>
