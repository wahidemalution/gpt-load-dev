<script setup lang="ts">
import LandingNavbar from './LandingNavbar.vue'
</script>

<template>
  <div class="modern-landing-layout">
    <LandingNavbar />
    <main class="modern-landing-content"><slot /></main>
  </div>
</template>

<style scoped>
.modern-landing-layout {
  min-height: 100dvh;
  background: var(--modern-subtle);
}
.modern-landing-content {
  display: grid;
  justify-items: center;
}
</style>
