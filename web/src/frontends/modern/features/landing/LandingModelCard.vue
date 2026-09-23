<script setup lang="ts">
import { computed } from 'vue'

import type { LandingPublicModel } from '@modern/api/landing-public'
import { numberFormatter } from '@modern/components/ui/intl-formatters'

const props = defineProps<{ model: LandingPublicModel }>()

const compact = numberFormatter('id-ID', { notation: 'compact' })
const price = new Intl.NumberFormat('en-US', {
  style: 'currency',
  currency: 'USD',
  currencyDisplay: 'narrowSymbol',
  minimumFractionDigits: 0,
  maximumFractionDigits: 9,
})

const context = computed(() =>
  props.model.context === null ? '—' : `${compact.format(props.model.context)} token`,
)
function formatPrice(value: string | null): string {
  return value === null ? '—' : price.format(Number(value))
}
const rows = computed(() => [
  { label: 'Input', value: formatPrice(props.model.prices.input) },
  { label: 'Output', value: formatPrice(props.model.prices.output) },
  { label: 'Cache Read', value: formatPrice(props.model.prices.cache_read) },
  { label: 'Cache Write', value: formatPrice(props.model.prices.cache_write) },
])
</script>

<template>
  <article class="modern-model-card">
    <h3 class="modern-model-card-name">{{ model.name }}</h3>
    <p class="modern-model-card-context">{{ context }}</p>
    <dl class="modern-model-card-prices">
      <div v-for="row in rows" :key="row.label" class="modern-model-card-price">
        <dt>{{ row.label }}</dt>
        <dd>{{ row.value }}</dd>
      </div>
    </dl>
  </article>
</template>

<style scoped>
.modern-model-card {
  display: grid;
  gap: var(--modern-space-3);
  align-content: start;
  border: var(--modern-line-width) solid var(--modern-border);
  border-radius: var(--modern-radius-panel);
  background: var(--modern-surface);
  padding: var(--modern-space-5);
}
.modern-model-card-name {
  overflow-wrap: anywhere;
  font-size: var(--modern-font-size-body);
  font-weight: var(--modern-weight-semibold);
}
.modern-model-card-context {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
}
.modern-model-card-prices {
  display: grid;
  gap: var(--modern-space-1-5);
  margin: 0;
}
.modern-model-card-price {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--modern-space-3);
  font-size: var(--modern-font-size-small);
}
.modern-model-card-price dt {
  color: var(--modern-muted);
}
.modern-model-card-price dd {
  margin: 0;
  font-variant-numeric: tabular-nums;
}
</style>
