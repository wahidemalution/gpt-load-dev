<script setup lang="ts">
import { ArrowLeft } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'

import { AppButton, AppIcon } from '@modern/components/ui'

const { t } = useI18n()
</script>

<template>
  <div class="modern-unavailable">
    <p class="modern-unavailable-code">404</p>
    <p class="modern-unavailable-description">{{ t('notFound.description') }}</p>
    <AppButton as-child>
      <RouterLink :to="{ name: 'modern-landing' }">
        <AppIcon :icon="ArrowLeft" size="sm" />{{ t('notFound.backHome') }}
      </RouterLink>
    </AppButton>
  </div>
</template>

<style scoped>
.modern-unavailable {
  display: grid;
  justify-items: center;
  gap: var(--modern-space-4);
  padding: var(--modern-space-12) var(--modern-content-inset);
  text-align: center;
}
.modern-unavailable-code {
  font-size: var(--modern-font-size-title);
  font-weight: var(--modern-weight-semibold);
}
.modern-unavailable-description {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-secondary);
}
.modern-unavailable a {
  display: inline-flex;
  align-items: center;
  gap: var(--modern-space-1-5);
  color: inherit;
  text-decoration: none;
}
</style>
