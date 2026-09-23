<script setup lang="ts">
import { DialogRoot } from 'reka-ui'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  listModels,
  updateKeyLimits,
  type ClientAreaModel,
  type OwnedKeyView,
} from '@modern/api/clientarea'
import {
  AppButton,
  AppDialogContent,
  AppDialogHeader,
  AppMultiSelect,
  AppNotice,
  AppTextField,
} from '@modern/components/ui'

import { useUserApiClient } from './client-area'

const props = defineProps<{ open: boolean; ownedKey: OwnedKeyView | null }>()
const emit = defineEmits<{ 'update:open': [open: boolean]; saved: [] }>()

const { t } = useI18n()
const client = useUserApiClient()

const rpm = ref('')
const concurrent = ref('')
const credit = ref('')
const models = ref<string[]>([])
const availableModels = ref<ClientAreaModel[]>([])
const submitting = ref(false)
const error = ref('')

const modelOptions = computed(() =>
  availableModels.value.map((model) => ({ value: model.name, label: model.name })),
)

watch(
  () => props.open,
  (open) => {
    if (!open) return
    error.value = ''
    submitting.value = false
    rpm.value = props.ownedKey ? String(props.ownedKey.rpm_limit) : ''
    concurrent.value = props.ownedKey ? String(props.ownedKey.max_concurrent) : ''
    credit.value = props.ownedKey ? String(props.ownedKey.credit_limit_nano_usd / 1e9) : ''
    models.value = props.ownedKey ? [...props.ownedKey.models] : []
    void loadModels()
  },
)

async function loadModels(): Promise<void> {
  try {
    availableModels.value = await listModels(client)
  } catch {
    availableModels.value = []
  }
}

function positiveInteger(value: string): number | undefined {
  const parsed = Number(value.trim())
  if (!Number.isSafeInteger(parsed) || parsed < 0) return undefined
  return parsed
}

async function submit(): Promise<void> {
  const target = props.ownedKey
  if (!target || submitting.value) return
  const rpmValue = positiveInteger(rpm.value)
  const concurrentValue = positiveInteger(concurrent.value)
  const creditValue = Number(credit.value.trim())
  if (rpmValue === undefined || concurrentValue === undefined || !Number.isFinite(creditValue) || creditValue < 0) {
    error.value = t('clientarea.keys.limits.saveFailed')
    return
  }
  submitting.value = true
  error.value = ''
  try {
    await updateKeyLimits(client, target.id, {
      rpm_limit: rpmValue,
      max_concurrent: concurrentValue,
      credit_limit_nano_usd: Math.round(creditValue * 1e9),
      models: models.value,
    })
    emit('saved')
    emit('update:open', false)
  } catch {
    error.value = t('clientarea.keys.limits.saveFailed')
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <DialogRoot :open="open" @update:open="(value: boolean) => emit('update:open', value)">
    <AppDialogContent
      :title="t('clientarea.keys.limits.title')"
      :description="t('clientarea.keys.limits.description')"
    >
      <AppDialogHeader
        :title="t('clientarea.keys.limits.title')"
        :description="t('clientarea.keys.limits.description')"
        :close-label="t('ui.close')"
        @close="emit('update:open', false)"
      />
      <form class="modern-clientarea-dialog-body" @submit.prevent="submit">
        <AppTextField
          v-model="rpm"
          :label="t('clientarea.keys.limits.rpm')"
          name="key-rpm"
          inputmode="numeric"
          :disabled="submitting"
          autocomplete="off"
        />
        <AppTextField
          v-model="concurrent"
          :label="t('clientarea.keys.limits.concurrent')"
          name="key-concurrent"
          inputmode="numeric"
          :disabled="submitting"
          autocomplete="off"
        />
        <AppTextField
          v-model="credit"
          :label="t('clientarea.keys.limits.credit')"
          name="key-credit"
          inputmode="decimal"
          :disabled="submitting"
          autocomplete="off"
        />
        <AppMultiSelect
          v-model="models"
          :label="t('clientarea.keys.limits.models')"
          :options="modelOptions"
          allow-custom
          :disabled="submitting"
        />
        <AppNotice v-if="error" tone="danger">{{ error }}</AppNotice>
        <div class="modern-clientarea-dialog-actions">
          <AppButton :disabled="submitting" @click="emit('update:open', false)">
            {{ t('ui.cancel') }}
          </AppButton>
          <AppButton variant="primary" type="submit" :loading="submitting">
            {{ t('clientarea.keys.limits.save') }}
          </AppButton>
        </div>
      </form>
    </AppDialogContent>
  </DialogRoot>
</template>

<style scoped>
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
