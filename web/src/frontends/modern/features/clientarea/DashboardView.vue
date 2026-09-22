<script setup lang="ts">
import { computed, onMounted, onScopeDispose, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { listKeys, usage } from '@modern/api/clientarea'
import { formatNanoUSD } from '@modern/app/format'
import { AppButton, AppCollectionState } from '@modern/components/ui'
import { numberFormatter } from '@modern/components/ui/intl-formatters'

import { useClientAreaShell, useUserApiClient } from './client-area'

const { t, locale } = useI18n()
const client = useUserApiClient()
const { profile } = useClientAreaShell()

const keyCount = ref<number | null>(null)
const requests = ref<number | null>(null)
const costNanoUSD = ref<number | null>(null)
const loading = ref(true)
const failed = ref(false)
let controller: AbortController | undefined

const number = computed(() => numberFormatter(locale.value))

async function load(): Promise<void> {
  controller?.abort()
  const request = new AbortController()
  controller = request
  loading.value = true
  failed.value = false
  const to = Date.now()
  const from = to - 30 * 86400000
  try {
    const [keys, rows] = await Promise.all([
      listKeys(client, request.signal),
      usage(client, { from_ms: from, to_ms: to }, request.signal),
    ])
    if (request.signal.aborted) return
    keyCount.value = keys.length
    requests.value = rows.reduce((total, row) => total + row.request_count, 0)
    costNanoUSD.value = rows.reduce((total, row) => total + row.estimated_cost_nano_usd, 0)
  } catch {
    if (!request.signal.aborted) failed.value = true
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
}

onMounted(load)
onScopeDispose(() => controller?.abort())

const stats = computed(() => [
  {
    key: 'balance',
    label: t('clientarea.dashboard.balance'),
    value:
      profile.value === null ? '—' : formatNanoUSD(profile.value.balance_nano_usd, locale.value),
  },
  {
    key: 'keys',
    label: t('clientarea.dashboard.keys'),
    value:
      keyCount.value === null || profile.value === null
        ? '—'
        : `${number.value.format(keyCount.value)} / ${number.value.format(profile.value.max_api_keys)}`,
  },
  {
    key: 'requests',
    label: t('clientarea.dashboard.requests30d'),
    value: requests.value === null ? '—' : number.value.format(requests.value),
  },
  {
    key: 'cost',
    label: t('clientarea.dashboard.cost30d'),
    value: costNanoUSD.value === null ? '—' : formatNanoUSD(costNanoUSD.value, locale.value),
  },
])
</script>

<template>
  <section class="modern-clientarea-dashboard">
    <header class="modern-clientarea-heading">
      <h1>{{ t('clientarea.dashboard.title') }}</h1>
      <p>{{ t('clientarea.dashboard.description') }}</p>
    </header>
    <AppCollectionState
      v-if="failed"
      :title="t('clientarea.dashboard.failed')"
      :description="t('clientarea.dashboard.failedDescription')"
      error
    >
      <AppButton @click="load">{{ t('ui.retry') }}</AppButton>
    </AppCollectionState>
    <div v-else class="modern-clientarea-stats" :aria-busy="loading">
      <div v-for="stat in stats" :key="stat.key" class="modern-clientarea-stat">
        <p class="modern-clientarea-stat-label">{{ stat.label }}</p>
        <p class="modern-clientarea-stat-value">{{ stat.value }}</p>
      </div>
    </div>
    <AppButton
      class="modern-clientarea-refresh"
      variant="text"
      size="sm"
      :loading="loading || undefined"
      @click="load"
    >
      {{ t('shell.refresh') }}
    </AppButton>
  </section>
</template>

<style scoped>
.modern-clientarea-dashboard {
  display: grid;
  gap: var(--modern-space-5);
}
.modern-clientarea-heading {
  display: grid;
  gap: var(--modern-space-1);
}
.modern-clientarea-heading h1 {
  font-size: var(--modern-font-size-section);
  font-weight: var(--modern-weight-semibold);
}
.modern-clientarea-heading p {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
}
.modern-clientarea-stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 200px), 1fr));
  gap: var(--modern-space-4);
}
.modern-clientarea-stat {
  display: grid;
  gap: var(--modern-space-1);
  border: var(--modern-line-width) solid var(--modern-border);
  border-radius: var(--modern-radius-panel);
  background: var(--modern-surface);
  padding: var(--modern-space-5);
}
.modern-clientarea-stat-label {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-caption);
  font-weight: var(--modern-weight-medium);
  letter-spacing: var(--modern-tracking-label);
  text-transform: uppercase;
}
.modern-clientarea-stat-value {
  font-size: var(--modern-font-size-title);
  font-weight: var(--modern-weight-semibold);
  font-variant-numeric: tabular-nums;
}
.modern-clientarea-refresh {
  justify-self: start;
}
</style>
