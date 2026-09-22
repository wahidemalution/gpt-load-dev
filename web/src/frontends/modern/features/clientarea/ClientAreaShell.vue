<script setup lang="ts">
import { RouterLink, RouterView } from 'vue-router'

// ponytail: placeholder — shell lengkap (saldo, nav, logout) dibangun di task berikutnya
</script>

<template>
  <div class="modern-clientarea-shell">
    <nav class="modern-clientarea-shell-nav" aria-label="Client area">
      <RouterLink :to="{ name: 'modern-clientarea-dashboard' }">Dashboard</RouterLink>
      <RouterLink :to="{ name: 'modern-clientarea-keys' }">Keys</RouterLink>
      <RouterLink :to="{ name: 'modern-clientarea-usage' }">Usage</RouterLink>
      <RouterLink :to="{ name: 'modern-clientarea-models' }">Models</RouterLink>
    </nav>
    <RouterView />
  </div>
</template>

<style scoped>
.modern-clientarea-shell {
  min-height: 100dvh;
}
.modern-clientarea-shell-nav {
  display: flex;
  gap: var(--modern-space-4);
  padding: var(--modern-space-4) var(--modern-content-inset);
}
</style>
