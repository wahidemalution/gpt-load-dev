<script setup lang="ts">
import { DialogRoot } from 'reka-ui'
import { computed, onMounted, onScopeDispose, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  createKey,
  deleteKey,
  listKeys,
  updateKeyStatus,
  type OwnedKeyView,
} from '@modern/api/clientarea'
import { formatNanoUSD } from '@modern/app/format'
import {
  AppBadge,
  AppButton,
  AppCollectionState,
  AppConfirmDialog,
  AppCopyValue,
  AppDialogContent,
  AppDialogHeader,
  AppNotice,
  AppTextField,
} from '@modern/components/ui'
import { dateFormatter } from '@modern/components/ui/intl-formatters'
import { ApiError } from '@shared/http/errors'

import { useClientAreaShell, useUserApiClient } from './client-area'
import KeyLimitsDialog from './KeyLimitsDialog.vue'

const { t, locale } = useI18n()
const client = useUserApiClient()
const { profile, refresh: refreshProfile } = useClientAreaShell()

const keys = ref<OwnedKeyView[]>([])
const loading = ref(true)
const failed = ref(false)
let controller: AbortController | undefined

const dialogOpen = ref(false)
const name = ref('')
const creating = ref(false)
const createError = ref('')
const revealed = ref('')

const disabling = ref<OwnedKeyView>()
const disablingBusy = ref(false)
const disableError = ref('')

const limitsTarget = ref<OwnedKeyView | null>(null)
const statusBusy = ref<Set<number>>(new Set())
const statusError = ref('')

const quotaReached = computed(
  () => profile.value !== null && keys.value.length >= profile.value.max_api_keys,
)
const format = computed(() =>
  dateFormatter(locale.value, { dateStyle: 'medium', timeStyle: 'short' }),
)

async function load(): Promise<void> {
  controller?.abort()
  const request = new AbortController()
  controller = request
  loading.value = true
  failed.value = false
  try {
    const rows = await listKeys(client, request.signal)
    if (!request.signal.aborted) keys.value = rows
  } catch {
    if (!request.signal.aborted) failed.value = true
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
}

onMounted(load)
onScopeDispose(() => controller?.abort())

function openCreate(): void {
  name.value = ''
  createError.value = ''
  dialogOpen.value = true
}

function closeReveal(): void {
  revealed.value = ''
}

async function create(): Promise<void> {
  if (creating.value) return
  creating.value = true
  createError.value = ''
  try {
    const result = await createKey(client, name.value.trim())
    revealed.value = result.key
    dialogOpen.value = false
    name.value = ''
    await Promise.all([load(), refreshProfile()])
  } catch (cause) {
    createError.value =
      cause instanceof ApiError && cause.status === 409
        ? t('clientarea.keys.quotaReached')
        : t('clientarea.keys.createFailed')
  } finally {
    creating.value = false
  }
}

async function confirmDisable(): Promise<void> {
  const target = disabling.value
  if (!target || disablingBusy.value) return
  disablingBusy.value = true
  disableError.value = ''
  try {
    await deleteKey(client, target.id)
    disabling.value = undefined
    await Promise.all([load(), refreshProfile()])
  } catch {
    disableError.value = t('clientarea.keys.disableFailed')
  } finally {
    disablingBusy.value = false
  }
}

function openLimits(row: OwnedKeyView): void {
  limitsTarget.value = row
}

async function toggleStatus(row: OwnedKeyView): Promise<void> {
  if (statusBusy.value.has(row.id)) return
  const next = row.status === 'active' ? 'disabled' : 'active'
  statusBusy.value = new Set(statusBusy.value).add(row.id)
  statusError.value = ''
  try {
    await updateKeyStatus(client, row.id, next)
    await load()
  } catch {
    statusError.value = t(
      next === 'active' ? 'clientarea.keys.enableFailed' : 'clientarea.keys.disableFailed',
    )
  } finally {
    const pending = new Set(statusBusy.value)
    pending.delete(row.id)
    statusBusy.value = pending
  }
}
</script>

<template>
  <section class="modern-clientarea-keys">
    <header class="modern-clientarea-heading">
      <div>
        <h1>{{ t('clientarea.keys.title') }}</h1>
        <p>{{ t('clientarea.keys.description') }}</p>
      </div>
      <AppButton variant="primary" :disabled="quotaReached" @click="openCreate">
        {{ t('clientarea.keys.create') }}
      </AppButton>
    </header>

    <AppNotice v-if="quotaReached" tone="warning">
      {{ t('clientarea.keys.quotaReached', { limit: profile?.max_api_keys }) }}
    </AppNotice>

    <AppCollectionState v-if="loading && keys.length === 0" :title="t('ui.loading')" loading />
    <AppCollectionState v-else-if="failed" :title="t('clientarea.keys.loadFailed')" error>
      <AppButton @click="load">{{ t('ui.retry') }}</AppButton>
    </AppCollectionState>
    <AppCollectionState
      v-else-if="keys.length === 0"
      :title="t('clientarea.keys.empty')"
      :description="t('clientarea.keys.emptyDescription')"
    />
    <div v-else class="modern-clientarea-table-frame">
      <table class="modern-clientarea-table">
        <thead>
          <tr>
            <th scope="col">{{ t('clientarea.keys.name') }}</th>
            <th scope="col">{{ t('clientarea.keys.key') }}</th>
            <th scope="col">{{ t('clientarea.keys.status') }}</th>
            <th scope="col">{{ t('clientarea.keys.created') }}</th>
            <th scope="col">{{ t('clientarea.keys.totalUsage') }}</th>
            <th scope="col" class="modern-clientarea-table-actions">
              {{ t('clientarea.keys.actions') }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in keys" :key="row.id">
            <td>{{ row.name }}</td>
            <td class="modern-clientarea-mono">{{ row.key_prefix }}…{{ row.key_suffix }}</td>
            <td>
              <AppBadge :tone="row.status === 'active' ? 'success' : 'neutral'">
                {{ t(`clientarea.keys.statuses.${row.status}`) }}
              </AppBadge>
            </td>
            <td>{{ format.format(row.created_at_ms) }}</td>
            <td class="modern-clientarea-mono">
              {{ formatNanoUSD(row.usage_total_nano_usd, locale) }}
            </td>
            <td class="modern-clientarea-table-actions">
              <AppButton variant="text" size="sm" @click="openLimits(row)">
                {{ t('clientarea.keys.limits.action') }}
              </AppButton>
              <AppButton
                variant="text"
                size="sm"
                :loading="statusBusy.has(row.id)"
                @click="toggleStatus(row)"
              >
                {{ row.status === 'active' ? t('clientarea.keys.disable') : t('clientarea.keys.enable') }}
              </AppButton>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <DialogRoot :open="dialogOpen" @update:open="(open: boolean) => (dialogOpen = open)">
      <AppDialogContent
        :title="t('clientarea.keys.createTitle')"
        :description="t('clientarea.keys.createDescription')"
      >
        <AppDialogHeader
          :title="t('clientarea.keys.createTitle')"
          :description="t('clientarea.keys.createDescription')"
          :close-label="t('ui.close')"
          @close="dialogOpen = false"
        />
        <form class="modern-clientarea-dialog-body" @submit.prevent="create">
          <AppTextField
            v-model="name"
            :label="t('clientarea.keys.name')"
            name="key-name"
            :placeholder="t('clientarea.keys.namePlaceholder')"
            :disabled="creating"
            autocomplete="off"
          />
          <AppNotice v-if="createError" tone="danger">{{ createError }}</AppNotice>
          <div class="modern-clientarea-dialog-actions">
            <AppButton :disabled="creating" @click="dialogOpen = false">
              {{ t('ui.cancel') }}
            </AppButton>
            <AppButton variant="primary" type="submit" :loading="creating">
              {{ t('clientarea.keys.create') }}
            </AppButton>
          </div>
        </form>
      </AppDialogContent>
    </DialogRoot>

    <DialogRoot
      :open="Boolean(revealed)"
      @update:open="
        (open: boolean) => {
          if (!open) closeReveal()
        }
      "
    >
      <AppDialogContent
        :title="t('clientarea.keys.revealTitle')"
        :description="t('clientarea.keys.revealDescription')"
      >
        <AppDialogHeader
          :title="t('clientarea.keys.revealTitle')"
          :description="t('clientarea.keys.revealDescription')"
          :close-label="t('ui.close')"
          @close="closeReveal"
        />
        <div class="modern-clientarea-dialog-body">
          <AppNotice tone="warning">{{ t('clientarea.keys.revealWarning') }}</AppNotice>
          <AppCopyValue :value="revealed" :label="t('ui.copy.action')" wrap />
          <div class="modern-clientarea-dialog-actions">
            <AppButton @click="closeReveal">{{ t('ui.close') }}</AppButton>
          </div>
        </div>
      </AppDialogContent>
    </DialogRoot>

    <AppNotice v-if="statusError" tone="danger">{{ statusError }}</AppNotice>

    <KeyLimitsDialog
      v-if="limitsTarget"
      :open="limitsTarget !== null"
      :owned-key="limitsTarget"
      @update:open="
        (open: boolean) => {
          if (!open) limitsTarget = null
        }
      "
      @saved="load"
    />

    <AppConfirmDialog
      :open="Boolean(disabling)"
      :title="t('clientarea.keys.disable')"
      :subject="disabling?.name"
      :description="t('clientarea.keys.disableDescription')"
      :confirm-label="t('clientarea.keys.disable')"
      tone="danger"
      :pending="disablingBusy"
      :error="disableError"
      @cancel="disabling = undefined"
      @confirm="confirmDisable"
    />
  </section>
</template>

<style scoped>
.modern-clientarea-keys {
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
.modern-clientarea-table th {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-caption);
  font-weight: var(--modern-weight-medium);
  letter-spacing: var(--modern-tracking-label);
  text-transform: uppercase;
}
.modern-clientarea-table tbody tr + tr td {
  border-top: var(--modern-line-width) solid var(--modern-border);
}
.modern-clientarea-mono {
  font-family: var(--modern-font-mono);
  font-variant-numeric: tabular-nums;
}
.modern-clientarea-table-actions {
  text-align: right;
}
.modern-clientarea-dialog-body {
  display: grid;
  gap: var(--modern-space-4);
  padding: var(--modern-space-5);
}
.modern-clientarea-dialog-actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--modern-space-2);
}
</style>
