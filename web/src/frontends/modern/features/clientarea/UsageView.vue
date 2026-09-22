<script setup lang="ts">
import { computed, onMounted, onScopeDispose, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { usage, type ClientAreaUsageRow } from '@modern/api/clientarea'
import { formatNanoUSD } from '@modern/app/format'
import { resolveTimeRange } from '@modern/app/time-range'
import { AppButton, AppCollectionState, AppSelect } from '@modern/components/ui'
import { dateRangePresets, type DateRangePreset } from '@modern/components/ui/date-time'
import { numberFormatter } from '@modern/components/ui/intl-formatters'

import { useUserApiClient } from './client-area'

const { t, locale } = useI18n()
const client = useUserApiClient()

const period = ref<string>('30d')
const rows = ref<ClientAreaUsageRow[]>([])
const loading = ref(true)
const failed = ref(false)
let controller: AbortController | undefined

const presetOptions = computed(() =>
  dateRangePresets.map((value) => ({ value, label: t(`ui.date.ranges.${value}`) })),
)
const preset = computed<DateRangePreset>(
  () => dateRangePresets.find((value) => value === period.value) ?? '30d',
)
const number = computed(() => numberFormatter(locale.value))
const requests = computed(() => rows.value.reduce((total, row) => total + row.request_count, 0))
const costNanoUSD = computed(() =>
  rows.value.reduce((total, row) => total + row.estimated_cost_nano_usd, 0),
)

async function load(): Promise<void> {
  controller?.abort()
  const request = new AbortController()
  controller = request
  loading.value = true
  failed.value = false
  const range = resolveTimeRange({ preset: preset.value })
  try {
    const result = await usage(
      client,
      { from_ms: Number(range.from_ms), to_ms: Number(range.to_ms) },
      request.signal,
    )
    if (!request.signal.aborted) rows.value = result
  } catch {
    if (!request.signal.aborted) failed.value = true
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
}

onMounted(load)
onScopeDispose(() => controller?.abort())
watch(period, load)
</script>

<template>
  <section class="modern-clientarea-usage">
    <header class="modern-clientarea-heading">
      <div>
        <h1>{{ t('clientarea.usage.title') }}</h1>
        <p>{{ t('clientarea.usage.description') }}</p>
      </div>
      <AppSelect
        v-model="period"
        class="modern-clientarea-period"
        :options="presetOptions"
        :label="t('clientarea.usage.period')"
        label-hidden
        size="sm"
      />
    </header>

    <AppCollectionState v-if="loading && rows.length === 0" :title="t('ui.loading')" loading />
    <AppCollectionState v-else-if="failed" :title="t('clientarea.usage.loadFailed')" error>
      <AppButton @click="load">{{ t('ui.retry') }}</AppButton>
    </AppCollectionState>
    <AppCollectionState
      v-else-if="rows.length === 0"
      :title="t('clientarea.usage.empty')"
      :description="t('clientarea.usage.emptyDescription')"
    />
    <div v-else class="modern-clientarea-table-frame">
      <table class="modern-clientarea-table">
        <thead>
          <tr>
            <th scope="col">{{ t('clientarea.usage.model') }}</th>
            <th scope="col">{{ t('clientarea.usage.requests') }}</th>
            <th scope="col">{{ t('clientarea.usage.cost') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in rows" :key="row.model">
            <td>{{ row.model }}</td>
            <td class="modern-clientarea-mono">{{ number.format(row.request_count) }}</td>
            <td class="modern-clientarea-mono">
              {{ formatNanoUSD(row.estimated_cost_nano_usd, locale) }}
            </td>
          </tr>
        </tbody>
        <tfoot>
          <tr>
            <th scope="row">{{ t('clientarea.usage.total') }}</th>
            <td class="modern-clientarea-mono">{{ number.format(requests) }}</td>
            <td class="modern-clientarea-mono">{{ formatNanoUSD(costNanoUSD, locale) }}</td>
          </tr>
        </tfoot>
      </table>
    </div>
  </section>
</template>

<style scoped>
.modern-clientarea-usage {
  display: grid;
  gap: var(--modern-space-5);
}
.modern-clientarea-heading {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--modern-space-3);
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
.modern-clientarea-period {
  min-width: 200px;
}
.modern-clientarea-table-frame {
  overflow-x: auto;
  border: var(--modern-line-width) solid var(--modern-border);
  border-radius: var(--modern-radius-panel);
  background: var(--modern-surface);
}
.modern-clientarea-table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--modern-font-size-small);
}
.modern-clientarea-table th,
.modern-clientarea-table td {
  padding: var(--modern-space-3) var(--modern-space-4);
  text-align: left;
  white-space: nowrap;
}
.modern-clientarea-table thead th {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-caption);
  font-weight: var(--modern-weight-medium);
  letter-spacing: var(--modern-tracking-label);
  text-transform: uppercase;
}
.modern-clientarea-table tbody tr + tr td {
  border-top: var(--modern-line-width) solid var(--modern-border);
}
.modern-clientarea-table tfoot th,
.modern-clientarea-table tfoot td {
  border-top: var(--modern-line-width) solid var(--modern-border);
  font-weight: var(--modern-weight-semibold);
}
.modern-clientarea-mono {
  font-family: var(--modern-font-mono);
  font-variant-numeric: tabular-nums;
}
</style>
