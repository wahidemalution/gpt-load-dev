<script setup lang="ts">
import { onMounted, onScopeDispose, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { listModels, type ClientAreaModel } from '@modern/api/clientarea'
import { AppButton, AppCollectionState } from '@modern/components/ui'
import LandingModelCard from '@modern/features/landing/LandingModelCard.vue'

import { useUserApiClient } from './client-area'

const { t } = useI18n()
const client = useUserApiClient()

const models = ref<ClientAreaModel[]>([])
const loading = ref(true)
const failed = ref(false)
let controller: AbortController | undefined

async function load(): Promise<void> {
  controller?.abort()
  const request = new AbortController()
  controller = request
  loading.value = true
  failed.value = false
  try {
    const result = await listModels(client, request.signal)
    if (!request.signal.aborted) models.value = result
  } catch {
    if (!request.signal.aborted) failed.value = true
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
}

onMounted(load)
onScopeDispose(() => controller?.abort())
</script>

<template>
  <section class="modern-clientarea-models">
    <header class="modern-clientarea-heading">
      <h1>{{ t('clientarea.models.title') }}</h1>
      <p>{{ t('clientarea.models.description') }}</p>
    </header>

    <AppCollectionState v-if="loading && models.length === 0" :title="t('ui.loading')" loading />
    <AppCollectionState v-else-if="failed" :title="t('clientarea.models.loadFailed')" error>
      <AppButton @click="load">{{ t('ui.retry') }}</AppButton>
    </AppCollectionState>
    <AppCollectionState v-else-if="models.length === 0" :title="t('clientarea.models.empty')" />
    <div v-else class="modern-clientarea-model-grid">
      <LandingModelCard v-for="model in models" :key="model.name" :model="model" />
    </div>
  </section>
</template>

<style scoped>
.modern-clientarea-models {
  display: grid;
  gap: var(--modern-space-5);
}
.modern-clientarea-heading h1 {
  font-size: var(--modern-font-size-section);
  font-weight: var(--modern-weight-semibold);
}
.modern-clientarea-heading p {
  margin-top: var(--modern-space-1);
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
}
.modern-clientarea-model-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 230px), 1fr));
  gap: var(--modern-space-4);
}
</style>
