<script setup lang="ts">
import { ArrowLeft, Ban, CircleCheck, CircleDollarSign, Users } from '@lucide/vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { DialogRoot } from 'reka-ui'
import { computed, onScopeDispose, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import {
  createAdjustment,
  getUser,
  listAdjustments,
  updateUser,
  type AdminUserDetail,
  type AdminUserKeyView,
} from '@modern/api/admin-users'
import { formatNanoUSD } from '@modern/app/format'
import { useMessages } from '@modern/app/messages'
import { usePageRefresh } from '@modern/app/page-refresh'
import {
  AppBadge,
  AppButton,
  AppCollectionState,
  AppConfirmDialog,
  AppDialogContent,
  AppDialogHeader,
  AppIconButton,
  AppInlineNumber,
  AppNotice,
  AppPanel,
  AppSelect,
  AppTextArea,
  AppTextField,
} from '@modern/components/ui'
import { dateFormatter } from '@modern/components/ui/intl-formatters'
import { useApiClient } from '@shared/http/client-context'

import AdminKeyLimitsDialog from './AdminKeyLimitsDialog.vue'

const { t, locale } = useI18n()
const client = useApiClient()
const route = useRoute()
const router = useRouter()
const cache = useQueryClient()
const messages = useMessages()
const controller = new AbortController()
const id = computed(() => Number(route.params.id))
const valid = computed(() => Number.isSafeInteger(id.value) && id.value > 0)
const query = useQuery(
  computed(() => ({
    queryKey: ['modern', 'admin-user', id.value],
    queryFn: ({ signal }: { signal: AbortSignal }) => getUser(client, id.value, signal),
    enabled: valid,
  })),
)
const adjustments = useQuery(
  computed(() => ({
    queryKey: ['modern', 'admin-user-adjustments', id.value],
    queryFn: ({ signal }: { signal: AbortSignal }) =>
      listAdjustments(client, id.value, { page_size: 100 }, signal),
    enabled: valid,
  })),
)
const profile = computed<AdminUserDetail | undefined>(() => query.data.value)
const adjustmentsRows = computed(() => adjustments.data.value?.items ?? [])
const format = computed(() =>
  dateFormatter(locale.value, { dateStyle: 'medium', timeStyle: 'short' }),
)
function formatDelta(value: number): string {
  return (value < 0 ? '-' : '+') + formatNanoUSD(Math.abs(value), locale.value)
}
function back(): void {
  void router.push({ name: 'modern-admin-users' })
}

const quotaPending = ref(false)
const quotaError = ref('')
async function saveQuota(value: number): Promise<void> {
  quotaPending.value = true
  quotaError.value = ''
  try {
    await updateUser(client, id.value, { max_api_keys: value }, controller.signal)
    if (controller.signal.aborted) return
    await query.refetch()
    void cache.invalidateQueries({ queryKey: ['modern', 'admin-users'] })
    messages.show({ text: t('users.detail.quotaSaved'), tone: 'success' })
  } catch {
    if (!controller.signal.aborted) quotaError.value = t('users.detail.quotaFailed')
  } finally {
    quotaPending.value = false
  }
}

const statusTarget = ref<'active' | 'disabled'>()
const statusPending = ref(false)
const statusError = ref('')
function openStatus(): void {
  if (!profile.value) return
  statusError.value = ''
  statusTarget.value = profile.value.status === 'active' ? 'disabled' : 'active'
}
async function confirmStatus(): Promise<void> {
  const target = statusTarget.value
  if (!target || statusPending.value) return
  statusPending.value = true
  statusError.value = ''
  try {
    await updateUser(client, id.value, { status: target }, controller.signal)
    if (controller.signal.aborted) return
    statusTarget.value = undefined
    await query.refetch()
    void cache.invalidateQueries({ queryKey: ['modern', 'admin-users'] })
    messages.show({ text: t('users.detail.statusSaved'), tone: 'success' })
  } catch {
    if (!controller.signal.aborted) statusError.value = t('users.detail.statusFailed')
  } finally {
    statusPending.value = false
  }
}

const limitsOpen = ref(false)
const limitsTarget = ref<AdminUserKeyView | null>(null)
function openLimits(key: AdminUserKeyView): void {
  limitsTarget.value = key
  limitsOpen.value = true
}
async function limitsSaved(): Promise<void> {
  await query.refetch()
  void cache.invalidateQueries({ queryKey: ['modern', 'admin-user', id.value] })
  messages.show({ text: t('users.detail.limitsSaved'), tone: 'success' })
}

const topupOpen = ref(false)
const topupPending = ref(false)
const topupError = ref('')
const topupDraft = ref({ amount: '', reason: 'topup', note: '' })
const reasonOptions = computed(() =>
  ['topup', 'correction'].map((value) => ({
    value,
    label: t('users.detail.reasons.' + value),
  })),
)
function openTopup(): void {
  topupDraft.value = { amount: '', reason: 'topup', note: '' }
  topupError.value = ''
  topupOpen.value = true
}
async function submitTopup(): Promise<void> {
  if (topupPending.value) return
  const raw = topupDraft.value.amount.trim()
  const usd = Number(raw)
  if (!raw || !Number.isFinite(usd) || Math.abs(usd) > 9_000_000) {
    topupError.value = t('users.detail.topupInvalid')
    return
  }
  const deltaNanoUSD = Math.round(usd * 1e9)
  if (deltaNanoUSD === 0) {
    topupError.value = t('users.detail.topupInvalid')
    return
  }
  topupPending.value = true
  topupError.value = ''
  try {
    await createAdjustment(
      client,
      id.value,
      {
        delta_nano_usd: deltaNanoUSD,
        reason: topupDraft.value.reason,
        ...(topupDraft.value.note.trim() ? { note: topupDraft.value.note.trim() } : {}),
      },
      controller.signal,
    )
    if (controller.signal.aborted) return
    topupOpen.value = false
    await Promise.all([query.refetch(), adjustments.refetch()])
    void cache.invalidateQueries({ queryKey: ['modern', 'admin-users'] })
    messages.show({ text: t('users.detail.topupSaved'), tone: 'success' })
  } catch {
    if (!controller.signal.aborted) topupError.value = t('users.detail.topupFailed')
  } finally {
    topupPending.value = false
  }
}

usePageRefresh({
  refresh: async () => {
    await Promise.all([query.refetch(), adjustments.refetch()])
  },
  pending: () => query.isFetching.value || adjustments.isFetching.value,
  updatedAt: () => query.dataUpdatedAt.value,
})
onScopeDispose(() => controller.abort())
</script>

<template>
  <div class="modern-user-workspace">
    <AppCollectionState
      v-if="valid && query.isPending.value"
      :title="t('collection.loading')"
      loading
    />
    <AppCollectionState
      v-else-if="!valid || !profile"
      :title="t(query.isError.value ? 'users.detail.loadFailed' : 'users.detail.notFound')"
      :error="query.isError.value"
      :icon="Users"
    >
      <AppButton v-if="query.isError.value" @click="query.refetch()">{{ t('ui.retry') }}</AppButton>
      <AppButton @click="back">{{ t('users.detail.back') }}</AppButton>
    </AppCollectionState>
    <template v-else>
      <header class="modern-user-header">
        <AppIconButton :icon="ArrowLeft" :label="t('users.detail.back')" @click="back" />
        <div class="modern-user-identity">
          <h1>{{ profile.email }}</h1>
          <AppBadge :tone="profile.status === 'active' ? 'success' : 'neutral'" dot>{{
            t('users.statuses.' + profile.status)
          }}</AppBadge>
        </div>
        <div class="modern-user-header-actions">
          <AppButton :icon="CircleDollarSign" variant="primary" @click="openTopup">{{
            t('users.detail.topup')
          }}</AppButton>
          <AppButton
            :icon="profile.status === 'active' ? Ban : CircleCheck"
            :variant="profile.status === 'active' ? 'danger' : 'default'"
            @click="openStatus"
            >{{
              t(profile.status === 'active' ? 'users.detail.disable' : 'users.detail.enable')
            }}</AppButton
          >
        </div>
      </header>

      <AppPanel :title="t('users.detail.profile')">
        <dl class="modern-user-facts">
          <div>
            <dt>{{ t('users.detail.balance') }}</dt>
            <dd class="modern-user-number">
              {{ formatNanoUSD(profile.balance_nano_usd, locale) }}
            </dd>
          </div>
          <div>
            <dt>{{ t('users.detail.created') }}</dt>
            <dd>{{ format.format(profile.created_at_ms) }}</dd>
          </div>
          <div>
            <dt>{{ t('users.detail.lastLogin') }}</dt>
            <dd>
              {{
                profile.last_login_at_ms
                  ? format.format(profile.last_login_at_ms)
                  : t('users.never')
              }}
            </dd>
          </div>
          <div>
            <dt>{{ t('users.detail.quota') }}</dt>
            <dd>
              <AppInlineNumber
                :model-value="profile.max_api_keys"
                :label="t('users.detail.quota')"
                :min="0"
                :max="100000"
                :pending="quotaPending"
                :error="quotaError"
                @submit="saveQuota"
                @clear-error="quotaError = ''"
              />
            </dd>
          </div>
        </dl>
        <p class="modern-user-hint">{{ t('users.detail.quotaHelp') }}</p>
      </AppPanel>

      <AppPanel :title="t('users.detail.ownedKeys')">
        <p v-if="!profile.keys.length" class="modern-user-hint">
          {{ t('users.detail.ownedKeysEmpty') }}
        </p>
        <div v-else class="modern-user-table-frame">
          <table class="modern-user-table">
            <thead>
              <tr>
                <th scope="col">{{ t('users.detail.keyName') }}</th>
                <th scope="col">{{ t('users.detail.key') }}</th>
                <th scope="col">{{ t('users.detail.keyStatus') }}</th>
                <th scope="col">{{ t('users.detail.keyRpm') }}</th>
                <th scope="col">{{ t('users.detail.keyConcurrent') }}</th>
                <th scope="col">{{ t('users.detail.keyCredit') }}</th>
                <th scope="col">{{ t('users.detail.keyModels') }}</th>
                <th scope="col">{{ t('accessKeys.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="key in profile.keys" :key="key.id">
                <td>{{ key.name }}</td>
                <td class="modern-user-mono">{{ key.key_prefix }}…{{ key.key_suffix }}</td>
                <td>
                  <AppBadge :tone="key.status === 'active' ? 'success' : 'neutral'" size="xs">{{
                    t('users.statuses.' + key.status)
                  }}</AppBadge>
                </td>
                <td class="modern-user-number">{{ key.rpm_limit || '—' }}</td>
                <td class="modern-user-number">{{ key.max_concurrent || '—' }}</td>
                <td class="modern-user-number">
                  {{
                    key.credit_limit_nano_usd > 0
                      ? formatNanoUSD(key.credit_limit_nano_usd, locale)
                      : '—'
                  }}
                </td>
                <td>
                  {{ key.models.length ? key.models.join(', ') : '—' }}
                </td>
                <td>
                  <AppButton size="xs" variant="ghost" @click="openLimits(key)">{{
                    t('users.detail.editLimits')
                  }}</AppButton>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </AppPanel>

      <AppPanel :title="t('users.detail.adjustments')">
        <AppCollectionState
          v-if="adjustments.isPending.value"
          :title="t('collection.loading')"
          loading
        />
        <AppCollectionState
          v-else-if="adjustments.isError.value && !adjustmentsRows.length"
          :title="t('users.detail.adjustmentsLoadFailed')"
          error
        >
          <AppButton @click="adjustments.refetch()">{{ t('ui.retry') }}</AppButton>
        </AppCollectionState>
        <p v-else-if="!adjustmentsRows.length" class="modern-user-hint">
          {{ t('users.detail.adjustmentsEmpty') }}
        </p>
        <div v-else class="modern-user-table-frame">
          <table class="modern-user-table">
            <thead>
              <tr>
                <th scope="col">{{ t('users.detail.adjustmentCreated') }}</th>
                <th scope="col">{{ t('users.detail.adjustmentDelta') }}</th>
                <th scope="col">{{ t('users.detail.adjustmentReason') }}</th>
                <th scope="col">{{ t('users.detail.adjustmentNote') }}</th>
                <th scope="col">{{ t('users.detail.adjustmentAdmin') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in adjustmentsRows" :key="row.id">
                <td>{{ format.format(row.created_at_ms) }}</td>
                <td class="modern-user-number" :class="{ 'is-negative': row.delta_nano_usd < 0 }">
                  {{ formatDelta(row.delta_nano_usd) }}
                </td>
                <td>{{ t('users.detail.reasons.' + row.reason) }}</td>
                <td>{{ row.admin_note || '—' }}</td>
                <td>{{ row.created_by_admin }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </AppPanel>
    </template>

    <AppConfirmDialog
      :open="Boolean(statusTarget)"
      :title="
        t(statusTarget === 'disabled' ? 'users.detail.disableTitle' : 'users.detail.enableTitle')
      "
      :subject="profile?.email"
      :description="
        t(statusTarget === 'disabled' ? 'users.detail.disableWarning' : 'users.detail.enableHelp')
      "
      :confirm-label="
        t(statusTarget === 'disabled' ? 'users.detail.disable' : 'users.detail.enable')
      "
      :tone="statusTarget === 'disabled' ? 'danger' : 'brand'"
      :pending="statusPending"
      :error="statusError"
      @cancel="statusTarget = undefined"
      @confirm="confirmStatus"
    />

    <DialogRoot :open="topupOpen" @update:open="(value: boolean) => (topupOpen = value)">
      <AppDialogContent
        :title="t('users.detail.topupTitle')"
        :description="t('users.detail.topupDescription')"
      >
        <AppDialogHeader
          :title="t('users.detail.topupTitle')"
          :description="t('users.detail.topupDescription')"
          :close-label="t('ui.close')"
          @close="topupOpen = false"
        />
        <form class="modern-user-dialog-body" @submit.prevent="submitTopup">
          <AppTextField
            v-model="topupDraft.amount"
            :label="t('users.detail.amount')"
            name="topup-amount"
            inputmode="decimal"
            placeholder="0.00"
            :description="t('users.detail.amountHint')"
            :disabled="topupPending"
            autocomplete="off"
          />
          <AppSelect
            v-model="topupDraft.reason"
            :label="t('users.detail.reason')"
            :options="reasonOptions"
            :disabled="topupPending"
          />
          <AppTextArea
            v-model="topupDraft.note"
            :label="t('users.detail.note')"
            name="topup-note"
            :placeholder="t('users.detail.notePlaceholder')"
            :rows="3"
            :disabled="topupPending"
          />
          <AppNotice v-if="topupError" tone="danger">{{ topupError }}</AppNotice>
          <div class="modern-user-dialog-actions">
            <AppButton :disabled="topupPending" @click="topupOpen = false">{{
              t('ui.cancel')
            }}</AppButton>
            <AppButton variant="primary" type="submit" :loading="topupPending">{{
              t('users.detail.topup')
            }}</AppButton>
          </div>
        </form>
      </AppDialogContent>
    </DialogRoot>

    <AdminKeyLimitsDialog v-model:open="limitsOpen" :owned-key="limitsTarget" @saved="limitsSaved" />
  </div>
</template>

<style scoped>
.modern-user-workspace {
  display: grid;
  flex: 1;
  min-width: 0;
  min-height: 0;
  align-content: start;
  gap: var(--modern-space-4);
}
.modern-user-header {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--modern-space-3);
}
.modern-user-identity {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--modern-space-2);
  min-width: 0;
}
.modern-user-identity h1 {
  font-size: var(--modern-font-size-title);
  font-weight: var(--modern-weight-semibold);
  letter-spacing: var(--modern-tracking-title);
  overflow-wrap: anywhere;
}
.modern-user-header-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--modern-space-2);
  margin-left: auto;
}
.modern-user-facts {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: var(--modern-space-3) var(--modern-space-4);
}
.modern-user-facts dt {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-caption);
  font-weight: var(--modern-weight-medium);
  letter-spacing: var(--modern-tracking-label);
  text-transform: uppercase;
}
.modern-user-facts dd {
  margin-top: var(--modern-space-1);
  font-size: var(--modern-font-size-secondary);
}
.modern-user-number {
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.modern-user-number.is-negative {
  color: var(--modern-danger);
}
.modern-user-hint {
  margin-top: var(--modern-space-3);
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
}
.modern-user-table-frame {
  overflow-x: auto;
  border: var(--modern-line-width) solid var(--modern-border);
  border-radius: var(--modern-radius-panel);
}
.modern-user-table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--modern-font-size-small);
}
.modern-user-table th,
.modern-user-table td {
  padding: var(--modern-space-3) var(--modern-space-4);
  text-align: left;
  white-space: nowrap;
}
.modern-user-table th {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-caption);
  font-weight: var(--modern-weight-medium);
  letter-spacing: var(--modern-tracking-label);
  text-transform: uppercase;
}
.modern-user-table tbody tr + tr td {
  border-top: var(--modern-line-width) solid var(--modern-border);
}
.modern-user-mono {
  font-family: var(--modern-font-mono);
}
.modern-user-dialog-body {
  display: grid;
  gap: var(--modern-space-4);
  padding: var(--modern-space-5);
}
.modern-user-dialog-actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--modern-space-2);
}
@media (max-width: 760px) {
  .modern-user-header-actions {
    margin-left: 0;
  }
}
</style>
