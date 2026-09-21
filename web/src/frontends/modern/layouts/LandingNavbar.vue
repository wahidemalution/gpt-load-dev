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
      <a href="#models">Model</a>
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
  display: flex;
  height: var(--modern-topbar-height);
  align-items: center;
  gap: var(--modern-space-4);
  border-bottom: var(--modern-line-width) solid var(--modern-border);
  background: var(--modern-surface);
  padding: 0 var(--modern-content-inset);
}
.modern-landing-navbar-brand {
  color: inherit;
  font-size: var(--modern-font-size-section);
  font-weight: var(--modern-weight-semibold);
  text-decoration: none;
}
.modern-landing-navbar-links {
  display: flex;
  align-items: center;
  gap: var(--modern-space-4);
  margin-inline: auto;
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
  display: flex;
  flex-shrink: 0;
  align-items: center;
  gap: var(--modern-space-2);
  margin-left: auto;
}
.modern-landing-navbar-actions :deep(a) {
  color: inherit;
  text-decoration: none;
}
@media (max-width: 760px) {
  .modern-landing-navbar-links {
    display: none;
  }
}
</style>
