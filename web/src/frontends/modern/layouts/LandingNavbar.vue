<script setup lang="ts">
import { RouterLink } from 'vue-router'

import { AppButton } from '@modern/components/ui'
import AppearanceMenu from './AppearanceMenu.vue'
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
      <AppButton variant="primary" as-child>
        <RouterLink :to="{ name: 'modern-login' }">Masuk</RouterLink>
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
