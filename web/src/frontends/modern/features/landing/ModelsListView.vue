<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'

import { getLandingPublic, type LandingPublicModel } from '@modern/api/landing-public'
import { AppButton } from '@modern/components/ui'
import { useApiClient } from '@shared/http/client-context'

import LandingModelCard from './LandingModelCard.vue'

const client = useApiClient()

const models = ref<LandingPublicModel[]>([])
const loading = ref(true)
const failed = ref(false)

let controller: AbortController | null = null

onMounted(async () => {
  controller = new AbortController()
  try {
    const data = await getLandingPublic(client, controller.signal)
    models.value = data.models
  } catch {
    failed.value = true
  } finally {
    loading.value = false
  }
})

onBeforeUnmount(() => {
  controller?.abort()
})
</script>

<template>
  <div class="modern-model-list">
    <header class="modern-model-list-header">
      <h1 class="modern-model-list-title">Semua Model</h1>
      <p class="modern-model-list-description">
        Daftar lengkap model yang tersedia beserta context window dan harga per token.
      </p>
    </header>
    <p v-if="failed || (!loading && models.length === 0)" class="modern-model-list-fallback">
      Model akan segera diumumkan.
    </p>
    <div v-else class="modern-model-list-grid">
      <LandingModelCard v-for="model in models" :key="model.name" :model="model" />
    </div>
    <div class="modern-model-list-back">
      <AppButton variant="outline" as-child>
        <RouterLink :to="{ name: 'modern-landing' }">Kembali ke Beranda</RouterLink>
      </AppButton>
    </div>
  </div>
</template>

<style scoped>
.modern-model-list {
  display: grid;
  width: min(100%, 1040px);
  gap: var(--modern-space-8);
  padding: var(--modern-space-8) var(--modern-content-inset);
}
.modern-model-list-header {
  display: grid;
  justify-items: center;
  gap: var(--modern-space-2);
  text-align: center;
}
.modern-model-list-title {
  font-size: var(--modern-font-size-title);
  font-weight: var(--modern-weight-semibold);
}
.modern-model-list-description {
  max-width: 40em;
  color: var(--modern-muted);
  font-size: var(--modern-font-size-body);
}
.modern-model-list-fallback {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-body);
}
.modern-model-list-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 230px), 1fr));
  gap: var(--modern-space-4);
}
.modern-model-list-back {
  display: flex;
  justify-content: center;
}
.modern-model-list-back a {
  color: inherit;
  text-decoration: none;
}
</style>
