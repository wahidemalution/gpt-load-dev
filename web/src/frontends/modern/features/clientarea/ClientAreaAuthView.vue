<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { AppButton, AppTextField } from '@modern/components/ui'
import { login, signup } from '@modern/api/clientarea'
import { useApiClient } from '@shared/http/client-context'
import { useUserSession } from './user-session'

const { t } = useI18n()
const client = useApiClient()
const session = useUserSession()

const mode = ref<'login' | 'signup'>('login')
const email = ref('')
const password = ref('')
const busy = ref(false)
const error = ref('')

async function submit(): Promise<void> {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    if (mode.value === 'signup') {
      await signup(client, email.value, password.value)
    }
    const result = await login(client, email.value, password.value)
    session.setSession(result.token, result.profile, result.expires_at_ms)
  } catch {
    error.value = t('clientarea.auth.failed')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <!-- ponytail: placeholder — auth UI dibangun di task berikutnya -->
  <section class="modern-clientarea-auth">
    <h1>{{ t('clientarea.auth.title') }}</h1>
    <p>{{ t('clientarea.auth.description') }}</p>
    <form class="modern-clientarea-auth-form" @submit.prevent="submit">
      <AppTextField v-model="email" :label="t('clientarea.auth.email')" type="email" required />
      <AppTextField
        v-model="password"
        :label="t('clientarea.auth.password')"
        type="password"
        required
      />
      <AppButton variant="primary" type="submit" :disabled="busy">
        {{ mode === 'login' ? t('clientarea.auth.login') : t('clientarea.auth.signup') }}
      </AppButton>
    </form>
    <p v-if="error" role="alert">{{ error }}</p>
    <AppButton variant="text" @click="mode = mode === 'login' ? 'signup' : 'login'">
      {{
        mode === 'login' ? t('clientarea.auth.switchToSignup') : t('clientarea.auth.switchToLogin')
      }}
    </AppButton>
  </section>
</template>

<style scoped>
.modern-clientarea-auth {
  display: grid;
  justify-items: center;
  gap: var(--modern-space-4);
  padding: var(--modern-space-12) var(--modern-content-inset);
  text-align: center;
}
.modern-clientarea-auth-form {
  display: grid;
  gap: var(--modern-space-3);
  width: min(100%, 320px);
}
</style>
