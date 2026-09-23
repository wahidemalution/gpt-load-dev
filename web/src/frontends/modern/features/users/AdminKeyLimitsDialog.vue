<script setup lang="ts">
import { DialogRoot } from 'reka-ui'
import { onScopeDispose, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  getAccessKey,
  updateAccessKey,
  type AccessInput,
  type AccessKey,
} from '@modern/api/access-keys'
import type { AdminUserKeyView } from '@modern/api/admin-users'
import { getModels } from '@modern/api/models'
import {
  AppButton,
  AppDialogContent,
  AppDialogHeader,
  AppMultiSelect,
  AppNotice,
  AppTextField,
} from '@modern/components/ui'
import { useApiClient } from '@shared/http/client-context'

const props = defineProps<{ open: boolean; ownedKey: AdminUserKeyView | null }>()
const emit = defineEmits<{ 'update:open': [open: boolean]; saved: [] }>()

const { t } = useI18n()
const client = useApiClient()
const controller = new AbortController()

const rpm = ref('')
const concurrent = ref('')
const credit = ref('')
const models = ref<string[]>([])
const modelOptions = ref<{ value: string; label: string }[]>([])
const base = ref<AccessKey | null>(null)
const submitting = ref(false)
const error = ref('')

watch(
  () => props.open,
  (open) => {
    if (!open) return
    error.value = ''
    submitting.value = false
    base.value = null
    rpm.value = props.ownedKey ? String(props.ownedKey.rpm_limit) : ''
    concurrent.value = props.ownedKey ? String(props.ownedKey.max_concurrent) : ''
    credit.value = props.ownedKey ? String(props.ownedKey.credit_limit_nano_usd / 1e9) : ''
    models.value = props.ownedKey ? [...props.ownedKey.models] : []
    void loadBase()
    void loadModels()
  },
)

async function loadBase(): Promise<void> {
  const target = props.ownedKey
  if (!target) return
  try {
    base.value = await getAccessKey(client, target.id, controller.signal)
  } catch {
    if (!controller.signal.aborted) base.value = null
  }
}

async function loadModels(): Promise<void> {
  try {
    const page = await getModels(
      client,
      { q: '', groups: 'enabled', pricing: 'all', page: 1, pageSize: 100 },
      controller.signal,
    )
    modelOptions.value = page.items.map((model) => ({ value: model.name, label: model.name }))
  } catch {
    modelOptions.value = []
  }
}

function nonNegativeInteger(value: string): number | undefined {
  if (!/^\d+$/.test(value.trim())) return undefined
  const parsed = Number(value.trim())
  return Number.isSafeInteger(parsed) ? parsed : undefined
}

function creditLimitNanoUSD(): number | undefined {
  const raw = credit.value.trim()
  if (!/^(0|[1-9]\d*)(\.\d{1,9})?$/.test(raw)) return undefined
  if (/^0(?:\.0+)?$/.test(raw)) return 0
  const nano = Math.round(Number(raw) * 1e9)
  return Number.isSafeInteger(nano) && nano > 0 ? nano : undefined
}

async function submit(): Promise<void> {
  const target = props.ownedKey
  const current = base.value
  if (!target || !current || submitting.value) return
  const rpmValue = nonNegativeInteger(rpm.value)
  const concurrentValue = nonNegativeInteger(concurrent.value)
  const creditNanoUSD = creditLimitNanoUSD()
  if (rpmValue === undefined || concurrentValue === undefined || creditNanoUSD === undefined) {
    error.value = t('users.detail.limitsFailed')
    return
  }
  const patch: Partial<AccessInput> = {}
  if (rpmValue !== current.rpm_limit) patch.rpm_limit = rpmValue
  if (concurrentValue !== current.max_concurrent) patch.max_concurrent = concurrentValue
  const currentCredit = current.cost_limit_rules.find((rule) => rule.kind === 'total')
  const currentCreditNanoUSD = currentCredit ? Math.round(Number(currentCredit.limit_usd) * 1e9) : 0
  if (creditNanoUSD !== currentCreditNanoUSD)
    patch.cost_limit_rules =
      creditNanoUSD > 0 ? [{ kind: 'total', limit_usd: String(creditNanoUSD / 1e9) }] : []
  if (models.value.join('\n') !== current.filters.models.join('\n'))
    patch.filters = { ...current.filters, models: models.value }
  if (!Object.keys(patch).length) {
    emit('update:open', false)
    return
  }
  submitting.value = true
  error.value = ''
  try {
    await updateAccessKey(client, target.id, patch, controller.signal)
    if (controller.signal.aborted) return
    emit('saved')
    emit('update:open', false)
  } catch {
    if (!controller.signal.aborted) error.value = t('users.detail.limitsFailed')
  } finally {
    submitting.value = false
  }
}

onScopeDispose(() => controller.abort())
</script>

<template>
  <DialogRoot :open="open" @update:open="(value: boolean) => emit('update:open', value)">
    <AppDialogContent :title="t('users.detail.editLimits')" :description="ownedKey?.name ?? ''">
      <AppDialogHeader
        :title="t('users.detail.editLimits')"
        :description="ownedKey?.name"
        :close-label="t('ui.close')"
        @close="emit('update:open', false)"
      />
      <form class="modern-user-dialog-body" @submit.prevent="submit">
        <AppTextField
          v-model="rpm"
          :label="t('users.detail.keyRpm')"
          name="key-rpm"
          inputmode="numeric"
          :disabled="submitting"
          autocomplete="off"
        />
        <AppTextField
          v-model="concurrent"
          :label="t('users.detail.keyConcurrent')"
          name="key-concurrent"
          inputmode="numeric"
          :disabled="submitting"
          autocomplete="off"
        />
        <AppTextField
          v-model="credit"
          :label="t('users.detail.keyCredit')"
          name="key-credit"
          inputmode="decimal"
          :disabled="submitting"
          autocomplete="off"
        />
        <AppMultiSelect
          v-model="models"
          :label="t('users.detail.keyModels')"
          :options="modelOptions"
          allow-custom
          :disabled="submitting"
        />
        <AppNotice v-if="error" tone="danger">{{ error }}</AppNotice>
        <div class="modern-user-dialog-actions">
          <AppButton :disabled="submitting" @click="emit('update:open', false)">
            {{ t('ui.cancel') }}
          </AppButton>
          <AppButton variant="primary" type="submit" :loading="submitting">
            {{ t('ui.save') }}
          </AppButton>
        </div>
      </form>
    </AppDialogContent>
  </DialogRoot>
</template>

<style scoped>
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
</style>
