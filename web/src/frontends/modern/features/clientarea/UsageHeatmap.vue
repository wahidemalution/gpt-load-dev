<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import type { ClientAreaDailyTokenRow } from '@modern/api/clientarea'
import { AppTooltip } from '@modern/components/ui'
import { dateFormatter, numberFormatter } from '@modern/components/ui/intl-formatters'

import { heatLevel, heatmapLevels } from './heatmap-display'

const props = defineProps<{
  rows: ClientAreaDailyTokenRow[]
  loading?: boolean
  failed?: boolean
}>()

const { t, locale } = useI18n()

interface HeatCell {
  day: number
  level: number
  value: number
}

const DAY_MS = 86400000
const WEEKS = 53
const LEVELS = heatmapLevels
// Weekday rows run Sunday..Saturday.
const WEEKDAY_KEYS = ['0', '1', '2', '3', '4', '5', '6'] as const

const byDay = computed(() => {
  const map = new Map<number, number>()
  for (const row of props.rows) map.set(row.day_ms, row.total_tokens)
  return map
})

const max = computed(() => props.rows.reduce((peak, row) => Math.max(peak, row.total_tokens), 0))

const number = computed(() => numberFormatter(locale.value))
const dayFormatter = computed(() =>
  dateFormatter(locale.value, { year: 'numeric', month: 'short', day: 'numeric' }),
)
const monthFormatter = computed(() => dateFormatter(locale.value, { month: 'short' }))

// 53 columns x 7 rows ending on today. Each column carries an optional month
// label shown when that column's first in-range day opens a new month.
const grid = computed(() => {
  const today = new Date()
  const todayUTC = Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), today.getUTCDate())
  const end = todayUTC + (6 - today.getUTCDay()) * DAY_MS
  const start = end - (WEEKS * 7 - 1) * DAY_MS
  const columns: Array<{ month: string | null; cells: Array<HeatCell | null> }> = []
  let lastMonth = -1
  for (let week = 0; week < WEEKS; week += 1) {
    const cells: Array<HeatCell | null> = []
    let month: string | null = null
    for (let weekday = 0; weekday < 7; weekday += 1) {
      const day = start + (week * 7 + weekday) * DAY_MS
      if (day > todayUTC) {
        cells.push(null)
        continue
      }
      const value = byDay.value.get(day) ?? 0
      cells.push({ day, level: heatLevel(value, max.value), value })
      const date = new Date(day)
      if (date.getUTCMonth() !== lastMonth) {
        lastMonth = date.getUTCMonth()
        if (week > 0) month = monthFormatter.value.format(date)
      }
    }
    columns.push({ month, cells })
  }
  return columns
})

const weekdayLabels = computed(() =>
  WEEKDAY_KEYS.map((key) => t(`clientarea.dashboard.heatmap.weekdays.${key}`)),
)

function tooltip(value: number, day: number): string {
  return t('clientarea.dashboard.heatmap.tooltip', {
    date: dayFormatter.value.format(new Date(day)),
    tokens: number.value.format(value),
  })
}
</script>

<template>
  <section class="modern-clientarea-heatmap">
    <header class="modern-clientarea-heatmap-heading">
      <h2>{{ t('clientarea.dashboard.heatmap.title') }}</h2>
      <p>{{ t('clientarea.dashboard.heatmap.description') }}</p>
    </header>
    <p v-if="failed" class="modern-clientarea-heatmap-state">
      {{ t('clientarea.dashboard.heatmap.failed') }}
    </p>
    <p v-else-if="loading" class="modern-clientarea-heatmap-state">{{ t('ui.loading') }}</p>
    <div v-else class="modern-clientarea-heatmap-frame">
      <div class="modern-clientarea-heatmap-month-row">
        <span
          v-for="(column, index) in grid"
          :key="index"
          class="modern-clientarea-heatmap-month"
          >{{ column.month ?? '' }}</span
        >
      </div>
      <div class="modern-clientarea-heatmap-body">
        <div class="modern-clientarea-heatmap-weekdays">
          <span v-for="label in weekdayLabels" :key="label">{{ label }}</span>
        </div>
        <div class="modern-clientarea-heatmap-weeks">
          <div
            v-for="(column, index) in grid"
            :key="index"
            class="modern-clientarea-heatmap-column"
          >
            <AppTooltip
              v-for="(cell, cellIndex) in column.cells"
              :key="cellIndex"
              :label="cell ? tooltip(cell.value, cell.day) : undefined"
              :disabled="!cell"
            >
              <span
                class="modern-clientarea-heatmap-cell"
                :class="`modern-clientarea-heatmap-cell--${cell?.level ?? 0}`"
                :aria-hidden="!cell"
              />
            </AppTooltip>
          </div>
        </div>
      </div>
      <div class="modern-clientarea-heatmap-legend">
        <span>{{ t('clientarea.dashboard.heatmap.less') }}</span>
        <span
          v-for="level in LEVELS + 1"
          :key="level"
          class="modern-clientarea-heatmap-cell"
          :class="`modern-clientarea-heatmap-cell--${level - 1}`"
        />
        <span>{{ t('clientarea.dashboard.heatmap.more') }}</span>
      </div>
    </div>
  </section>
</template>

<style scoped>
.modern-clientarea-heatmap {
  display: grid;
  gap: var(--modern-space-3);
  border: var(--modern-line-width) solid var(--modern-border);
  border-radius: var(--modern-radius-panel);
  background: var(--modern-surface);
  padding: var(--modern-space-5);
}
.modern-clientarea-heatmap-heading h2 {
  font-size: var(--modern-font-size-title);
  font-weight: var(--modern-weight-semibold);
}
.modern-clientarea-heatmap-heading p {
  margin-top: var(--modern-space-1);
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
}
.modern-clientarea-heatmap-state {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
}
/* The grid grows to fill the panel width; every column gets an equal fraction
   so the full history reaches both edges without a horizontal scrollbar. */
.modern-clientarea-heatmap-frame {
  display: grid;
  gap: var(--modern-space-2);
  min-width: 0;
}
.modern-clientarea-heatmap-month-row {
  display: grid;
  grid-template-columns: repeat(53, minmax(0, 1fr));
  gap: var(--modern-heatmap-gap);
  margin-left: calc(var(--modern-heatmap-weekday-col) + var(--modern-space-2));
}
.modern-clientarea-heatmap-month {
  overflow: visible;
  color: var(--modern-muted);
  font-size: var(--modern-font-size-caption);
  line-height: var(--modern-heatmap-label);
  white-space: nowrap;
}
.modern-clientarea-heatmap-body {
  display: flex;
  gap: var(--modern-space-2);
  min-width: 0;
}
.modern-clientarea-heatmap-weekdays {
  display: grid;
  grid-template-rows: repeat(7, minmax(0, 1fr));
  gap: var(--modern-heatmap-gap);
  width: var(--modern-heatmap-weekday-col);
  flex: none;
  color: var(--modern-muted);
  font-size: var(--modern-font-size-caption);
  line-height: var(--modern-heatmap-label);
}
.modern-clientarea-heatmap-weekdays span {
  display: flex;
  align-items: center;
}
.modern-clientarea-heatmap-weeks {
  display: grid;
  grid-template-columns: repeat(53, minmax(0, 1fr));
  gap: var(--modern-heatmap-gap);
  flex: 1;
  min-width: 0;
}
.modern-clientarea-heatmap-column {
  display: grid;
  grid-template-rows: repeat(7, minmax(0, 1fr));
  gap: var(--modern-heatmap-gap);
  min-width: 0;
}
.modern-clientarea-heatmap-cell {
  width: 100%;
  aspect-ratio: 1;
  border-radius: var(--modern-radius-small);
  background: var(--modern-heatmap-0);
}
.modern-clientarea-heatmap-cell--0 {
  background: var(--modern-heatmap-0);
}
.modern-clientarea-heatmap-cell--1 {
  background: color-mix(in srgb, var(--modern-accent) 25%, var(--modern-heatmap-0));
}
.modern-clientarea-heatmap-cell--2 {
  background: color-mix(in srgb, var(--modern-accent) 50%, var(--modern-heatmap-0));
}
.modern-clientarea-heatmap-cell--3 {
  background: color-mix(in srgb, var(--modern-accent) 75%, var(--modern-heatmap-0));
}
.modern-clientarea-heatmap-cell--4 {
  background: var(--modern-accent);
}
.modern-clientarea-heatmap-legend {
  display: flex;
  align-items: center;
  gap: var(--modern-space-1);
  color: var(--modern-muted);
  font-size: var(--modern-font-size-caption);
}
.modern-clientarea-heatmap-legend .modern-clientarea-heatmap-cell {
  width: var(--modern-heatmap-cell);
  aspect-ratio: auto;
  height: var(--modern-heatmap-cell);
}
</style>
