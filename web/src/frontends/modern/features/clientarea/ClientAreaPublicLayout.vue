<script setup lang="ts">
import LandingNavbar from '@modern/layouts/LandingNavbar.vue'
</script>

<template>
  <div class="modern-clientarea-layout">
    <LandingNavbar />
    <main class="modern-clientarea-content"><slot /></main>
  </div>
</template>

<style scoped>
.modern-clientarea-layout {
  min-height: 100dvh;
  background: var(--modern-subtle);
}
.modern-clientarea-content {
  display: grid;
  justify-items: center;
}
</style>
