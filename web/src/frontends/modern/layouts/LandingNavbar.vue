<script setup lang="ts">
import { LogOut } from '@lucide/vue'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRoute, useRouter } from 'vue-router'

import { logout } from '@modern/api/clientarea'
import { AppButton } from '@modern/components/ui'
import { useUserApiClient } from '@modern/features/clientarea/client-area'
import { useUserSession } from '@modern/features/clientarea/user-session'
import AppearanceMenu from './AppearanceMenu.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const client = useUserApiClient()
const session = useUserSession()

const authenticated = computed(() => session.state.token !== '')
const loggingOut = ref(false)

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
    // Private client-area pages render an inline 404 once anonymous, so leave
    // them for the sign-in view. Public pages stay put and re-render inline.
    if (route.meta.requiresUserAuth) await router.replace({ name: 'modern-clientarea-home' })
  }
}
</script>

<template>
  <header class="modern-landing-navbar">
    <RouterLink class="modern-landing-navbar-brand" :to="{ name: 'modern-landing' }">
      Tokenizer
    </RouterLink>
    <!-- ponytail: no hamburger on mobile — anchors still reachable via scroll; add menu only if requested -->
    <nav class="modern-landing-navbar-links" aria-label="Landing">
      <RouterLink :to="{ name: 'modern-model-list' }">Model</RouterLink>
      <a href="#harga">Harga</a>
      <a href="#faq">FAQ</a>
    </nav>
    <div class="modern-landing-navbar-actions">
      <AppearanceMenu />
      <AppButton
        v-if="authenticated"
        variant="outline"
        size="sm"
        :icon="LogOut"
        :loading="loggingOut"
        @click="signOut"
      >
        {{ t('clientarea.logout') }}
      </AppButton>
      <AppButton v-else variant="primary" as-child>
        <RouterLink :to="{ name: 'modern-clientarea-home' }">Masuk</RouterLink>
      </AppButton>
    </div>
  </header>
</template>

<style scoped>
.modern-landing-navbar {
  position: sticky;
  top: 0;
  z-index: 20;
  display: grid;
  grid-template-columns: 1fr auto 1fr;
  height: var(--modern-topbar-height);
  align-items: center;
  gap: var(--modern-space-4);
  border-bottom: var(--modern-line-width) solid var(--modern-border);
  background: var(--modern-surface);
  padding: 0 var(--modern-content-inset);
}
.modern-landing-navbar > * {
  display: flex;
  align-items: center;
  min-width: 0;
}
.modern-landing-navbar-brand {
  justify-self: start;
  color: inherit;
  font-size: var(--modern-font-size-section);
  font-weight: var(--modern-weight-semibold);
  text-decoration: none;
}
.modern-landing-navbar-links {
  justify-self: center;
  gap: var(--modern-space-4);
}
.modern-landing-navbar-links a {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
  text-decoration: none;
}
.modern-landing-navbar-links a:hover {
  color: var(--modern-accent);
}
.modern-landing-navbar-actions {
  justify-self: end;
  gap: var(--modern-space-2);
}
.modern-landing-navbar-actions :deep(a) {
  color: inherit;
  text-decoration: none;
}
@media (max-width: 760px) {
  .modern-landing-navbar {
    grid-template-columns: 1fr auto;
  }
  .modern-landing-navbar-links {
    display: none;
  }
}
</style>
