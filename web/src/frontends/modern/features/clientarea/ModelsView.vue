<script setup lang="ts">
import { computed, onMounted, onScopeDispose, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { listModels, type ClientAreaModel } from '@modern/api/clientarea'
import { AppButton, AppCollectionState } from '@modern/components/ui'
import { formatCompactNumber } from '@modern/components/ui/format'
import { numberFormatter } from '@modern/components/ui/intl-formatters'

import { useUserApiClient } from './client-area'

const { t, locale } = useI18n()
const client = useUserApiClient()

const models = ref<ClientAreaModel[]>([])
const loading = ref(true)
const failed = ref(false)
let controller: AbortController | undefined

const priceFormatter = computed(() =>
  numberFormatter(locale.value, {
    style: 'currency',
    currency: 'USD',
    currencyDisplay: 'narrowSymbol',
    minimumFractionDigits: 0,
    maximumFractionDigits: 9,
  }),
)

function formatPrice(value: string | null): string {
  return value === null ? '—' : priceFormatter.value.format(Number(value))
}

function formatContext(context: number | null): string {
  return context === null
    ? '—'
    : t('clientarea.models.context', { value: formatCompactNumber(context, locale.value) })
}

function priceRows(model: ClientAreaModel) {
  return [
    { label: t('clientarea.models.input'), value: formatPrice(model.prices.input) },
    { label: t('clientarea.models.output'), value: formatPrice(model.prices.output) },
    { label: t('clientarea.models.cacheRead'), value: formatPrice(model.prices.cache_read) },
    { label: t('clientarea.models.cacheWrite'), value: formatPrice(model.prices.cache_write) },
  ]
}

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
      <article v-for="model in models" :key="model.name" class="modern-clientarea-model-card">
        <h2 class="modern-clientarea-model-name">{{ model.name }}</h2>
        <p class="modern-clientarea-model-context">{{ formatContext(model.context) }}</p>
        <dl class="modern-clientarea-model-prices">
          <div
            v-for="row in priceRows(model)"
            :key="row.label"
            class="modern-clientarea-model-price"
          >
            <dt>{{ row.label }}</dt>
            <dd>{{ row.value }}</dd>
          </div>
        </dl>
      </article>
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
.modern-clientarea-model-card {
  display: grid;
  gap: var(--modern-space-3);
  align-content: start;
  border: var(--modern-line-width) solid var(--modern-border);
  border-radius: var(--modern-radius-panel);
  background: var(--modern-surface);
  padding: var(--modern-space-5);
}
.modern-clientarea-model-name {
  overflow-wrap: anywhere;
  font-size: var(--modern-font-size-body);
  font-weight: var(--modern-weight-semibold);
}
.modern-clientarea-model-context {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
}
.modern-clientarea-model-prices {
  display: grid;
  gap: var(--modern-space-1-5);
  margin: 0;
}
.modern-clientarea-model-price {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--modern-space-3);
  font-size: var(--modern-font-size-small);
}
.modern-clientarea-model-price dt {
  color: var(--modern-muted);
}
.modern-clientarea-model-price dd {
  margin: 0;
  font-variant-numeric: tabular-nums;
}
</style>
