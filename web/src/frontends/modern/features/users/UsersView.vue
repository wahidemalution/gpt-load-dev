<script setup lang="ts">
import { Search, UserPlus, Users } from '@lucide/vue'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/vue-query'
import { DialogRoot } from 'reka-ui'
import { computed, onScopeDispose, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import { createUser, listUsers } from '@modern/api/admin-users'
import { formatNanoUSD } from '@modern/app/format'
import { useMessages, useMessageSource } from '@modern/app/messages'
import { usePageRefresh } from '@modern/app/page-refresh'
import { positivePage, useURLState } from '@modern/app/url-state'
import {
  AppBadge,
  AppButton,
  AppCollectionState,
  AppCopyValue,
  AppDialogContent,
  AppDialogHeader,
  AppListFrame,
  AppNotice,
  AppPagination,
  AppTextField,
} from '@modern/components/ui'
import { dateFormatter } from '@modern/components/ui/intl-formatters'
import { useApiClient } from '@shared/http/client-context'
import { ApiError } from '@shared/http/errors'

interface UserFilters {
  q: string
  page: number
  pageSize: number
}

const { t, locale } = useI18n()
const client = useApiClient()
const route = useRoute()
const router = useRouter()
const cache = useQueryClient()
const messages = useMessages()
const controller = new AbortController()
const frame = ref<InstanceType<typeof AppListFrame>>()
const filters = useURLState<UserFilters>(
  ['q', 'page', 'page_size'],
  (query) => ({
    q: typeof query.q === 'string' ? Array.from(query.q.trim()).slice(0, 200).join('') : '',
    page: positivePage(query.page),
    pageSize: [20, 50, 100].includes(Number(query.page_size)) ? Number(query.page_size) : 20,
  }),
  (value) => ({
    ...(value.q ? { q: value.q } : {}),
    ...(value.page > 1 ? { page: String(value.page) } : {}),
    ...(value.pageSize !== 20 ? { page_size: String(value.pageSize) } : {}),
  }),
)
const search = ref(filters.value.q)
const composing = ref(false)
let timer: ReturnType<typeof setTimeout> | undefined
const query = useQuery(
  computed(() => ({
    queryKey: ['modern', 'admin-users', filters.value],
    queryFn: ({ signal }: { signal: AbortSignal }) =>
      listUsers(
        client,
        {
          page: filters.value.page,
          page_size: filters.value.pageSize,
          ...(filters.value.q ? { search: filters.value.q } : {}),
        },
        signal,
      ),
    placeholderData: keepPreviousData,
  })),
)
const rows = computed(() => query.data.value?.items ?? [])
const outOfRange = computed(() => !rows.value.length && (query.data.value?.total ?? 0) > 0)
const format = computed(() =>
  dateFormatter(locale.value, { dateStyle: 'medium', timeStyle: 'short' }),
)

const dialogOpen = ref(false)
const creating = ref(false)
const createError = ref('')
const revealed = ref('')
const draft = ref({ email: '', password: '', maxKeys: '', balance: '' })

function change(patch: Partial<UserFilters>): void {
  filters.value = { ...filters.value, page: 1, ...patch }
  frame.value?.scrollToTop()
}
watch(
  () => filters.value.q,
  (value) => {
    clearTimeout(timer)
    search.value = value
  },
)
function scheduleSearch(): void {
  clearTimeout(timer)
  if (!composing.value) timer = setTimeout(() => change({ q: search.value.trim() }), 200)
}
function endComposition(): void {
  composing.value = false
  scheduleSearch()
}
function open(row: { id: number }): void {
  void router.push({ name: 'modern-admin-user-detail', params: { id: String(row.id) } })
}
function openCreate(): void {
  draft.value = { email: '', password: '', maxKeys: '', balance: '' }
  createError.value = ''
  dialogOpen.value = true
}
function closeReveal(): void {
  revealed.value = ''
}
async function submitCreate(): Promise<void> {
  if (creating.value) return
  const email = draft.value.email.trim()
  if (!email) {
    createError.value = t('users.invalid')
    return
  }
  const maxKeys = draft.value.maxKeys.trim()
  const maxApiKeys = maxKeys ? Number(maxKeys) : undefined
  if (maxApiKeys !== undefined && (!Number.isSafeInteger(maxApiKeys) || maxApiKeys < 0)) {
    createError.value = t('users.invalid')
    return
  }
  const rawBalance = draft.value.balance.trim()
  const usd = rawBalance ? Number(rawBalance) : undefined
  if (usd !== undefined && (!Number.isFinite(usd) || usd < 0 || usd > 9_000_000)) {
    createError.value = t('users.invalid')
    return
  }
  creating.value = true
  createError.value = ''
  try {
    const result = await createUser(
      client,
      {
        email,
        ...(draft.value.password ? { password: draft.value.password } : {}),
        ...(maxApiKeys !== undefined && maxApiKeys > 0 ? { max_api_keys: maxApiKeys } : {}),
        ...(usd !== undefined ? { balance_nano_usd: Math.round(usd * 1e9) } : {}),
      },
      controller.signal,
    )
    if (controller.signal.aborted) return
    dialogOpen.value = false
    revealed.value = result.initial_password ?? ''
    messages.show({ text: t('users.created', { email: result.profile.email }), tone: 'success' })
    await cache.invalidateQueries({ queryKey: ['modern', 'admin-users'] })
  } catch (cause) {
    if (!controller.signal.aborted)
      createError.value =
        cause instanceof ApiError && cause.status === 409
          ? t('users.duplicate')
          : cause instanceof ApiError && cause.status === 400
            ? t('users.invalid')
            : t('users.createFailed')
  } finally {
    creating.value = false
  }
}
usePageRefresh({
  refresh: () => query.refetch(),
  pending: () => query.isFetching.value,
  updatedAt: () => query.dataUpdatedAt.value,
})
useMessageSource(() =>
  query.isError.value && query.data.value
    ? {
        text: t('users.stale'),
        tone: 'warning',
        action: { label: t('ui.retry'), run: () => query.refetch() },
      }
    : undefined,
)
onScopeDispose(() => {
  controller.abort()
  clearTimeout(timer)
})
</script>

<template>
  <div class="modern-users-workspace">
    <form class="modern-users-toolbar" role="search" @submit.prevent="change({ q: search.trim() })">
      <AppTextField
        v-model="search"
        class="modern-users-search"
        :label="t('users.search')"
        label-hidden
        :placeholder="t('users.search')"
        :icon="Search"
        type="search"
        @input="scheduleSearch"
        @compositionstart="composing = true"
        @compositionend="endComposition"
      />
      <div class="modern-users-toolbar-actions">
        <AppButton :icon="UserPlus" variant="primary" @click="openCreate">{{
          t('users.create')
        }}</AppButton>
      </div>
    </form>
    <AppListFrame
      ref="frame"
      :label="t('users.title')"
      :scroll-key="route.fullPath"
      :loading="query.isFetching.value && Boolean(query.data.value)"
    >
      <template #header>
        <div class="modern-users-row modern-users-head" aria-hidden="true">
          <span>{{ t('users.columns.email') }}</span>
          <span>{{ t('users.columns.status') }}</span>
          <span>{{ t('users.columns.balance') }}</span>
          <span>{{ t('users.columns.keys') }}</span>
          <span>{{ t('users.columns.created') }}</span>
          <span>{{ t('users.columns.lastLogin') }}</span>
        </div>
      </template>
      <AppCollectionState v-if="query.isPending.value" :title="t('collection.loading')" loading />
      <AppCollectionState v-else-if="!query.data.value" :title="t('users.loadFailed')" error>
        <AppButton @click="query.refetch()">{{ t('ui.retry') }}</AppButton>
      </AppCollectionState>
      <AppCollectionState
        v-else-if="outOfRange"
        :title="t('users.outOfRange')"
        :description="t('users.outOfRangeHelp')"
        :icon="Users"
      />
      <AppCollectionState
        v-else-if="!rows.length"
        :title="t(filters.q ? 'users.noResults' : 'users.empty')"
        :description="filters.q ? undefined : t('users.emptyHelp')"
        :icon="Users"
      />
      <article
        v-for="row in rows"
        v-else
        :key="row.id"
        class="modern-users-row modern-users-item"
        :aria-label="row.email"
      >
        <div class="modern-users-email">
          <AppButton variant="text" @click="open(row)">{{ row.email }}</AppButton>
        </div>
        <div>
          <AppBadge
            :tone="row.status === 'active' ? 'success' : 'neutral'"
            variant="plain"
            size="xs"
            dot
            >{{ t('users.statuses.' + row.status) }}</AppBadge
          >
        </div>
        <div class="modern-users-number">{{ formatNanoUSD(row.balance_nano_usd, locale) }}</div>
        <div class="modern-users-number">
          {{ t('users.quota', { used: row.key_count, limit: row.max_api_keys }) }}
        </div>
        <div class="modern-users-time">{{ format.format(row.created_at_ms) }}</div>
        <div class="modern-users-time">
          {{ row.last_login_at_ms ? format.format(row.last_login_at_ms) : t('users.never') }}
        </div>
      </article>
      <template #footer>
        <AppPagination
          :page="filters.page"
          :page-size="filters.pageSize"
          mode="total"
          :total="query.data.value?.total"
          :pending="query.isFetching.value"
          @update:page="change({ page: $event })"
          @update:page-size="change({ pageSize: $event })"
        />
      </template>
    </AppListFrame>
  </div>

  <DialogRoot :open="dialogOpen" @update:open="(value: boolean) => (dialogOpen = value)">
    <AppDialogContent :title="t('users.createTitle')" :description="t('users.createDescription')">
      <AppDialogHeader
        :title="t('users.createTitle')"
        :description="t('users.createDescription')"
        :close-label="t('ui.close')"
        @close="dialogOpen = false"
      />
      <form class="modern-users-dialog-body" @submit.prevent="submitCreate">
        <AppTextField
          v-model="draft.email"
          :label="t('users.email')"
          name="user-email"
          type="email"
          :placeholder="t('users.emailPlaceholder')"
          :disabled="creating"
          autocomplete="off"
        />
        <AppTextField
          v-model="draft.password"
          :label="t('users.password')"
          name="user-password"
          type="password"
          :description="t('users.passwordHint')"
          :disabled="creating"
          autocomplete="new-password"
        />
        <AppTextField
          v-model="draft.maxKeys"
          :label="t('users.maxApiKeys')"
          name="user-max-keys"
          inputmode="numeric"
          :description="t('users.maxApiKeysHint')"
          :disabled="creating"
        />
        <AppTextField
          v-model="draft.balance"
          :label="t('users.initialBalance')"
          name="user-balance"
          inputmode="decimal"
          placeholder="0.00"
          :description="t('users.initialBalanceHint')"
          :disabled="creating"
        />
        <AppNotice v-if="createError" tone="danger">{{ createError }}</AppNotice>
        <div class="modern-users-dialog-actions">
          <AppButton :disabled="creating" @click="dialogOpen = false">{{
            t('ui.cancel')
          }}</AppButton>
          <AppButton variant="primary" type="submit" :loading="creating">{{
            t('users.create')
          }}</AppButton>
        </div>
      </form>
    </AppDialogContent>
  </DialogRoot>

  <DialogRoot
    :open="Boolean(revealed)"
    @update:open="
      (value: boolean) => {
        if (!value) closeReveal()
      }
    "
  >
    <AppDialogContent
      :title="t('users.initialPasswordTitle')"
      :description="t('users.initialPasswordDescription')"
    >
      <AppDialogHeader
        :title="t('users.initialPasswordTitle')"
        :description="t('users.initialPasswordDescription')"
        :close-label="t('ui.close')"
        @close="closeReveal"
      />
      <div class="modern-users-dialog-body">
        <AppNotice tone="warning">{{ t('users.initialPasswordWarning') }}</AppNotice>
        <AppCopyValue :value="revealed" :label="t('ui.copy.action')" wrap />
        <div class="modern-users-dialog-actions">
          <AppButton @click="closeReveal">{{ t('ui.close') }}</AppButton>
        </div>
      </div>
    </AppDialogContent>
  </DialogRoot>
</template>

<style scoped>
.modern-users-workspace {
  display: flex;
  flex: 1;
  min-width: 0;
  min-height: 0;
  flex-direction: column;
}
.modern-users-toolbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  flex: none;
  gap: var(--modern-space-3);
  padding: var(--modern-space-5) 0 var(--modern-space-3);
}
.modern-users-search {
  flex: 2 1 240px;
  min-width: 0;
}
.modern-users-toolbar-actions {
  display: flex;
  align-items: center;
  gap: var(--modern-space-2);
  margin-left: auto;
}
.modern-users-row {
  display: grid;
  grid-template-columns:
    minmax(220px, 2fr) 96px 132px 120px minmax(148px, 1fr)
    minmax(148px, 1fr);
  align-items: center;
  gap: var(--modern-space-3);
  min-width: 960px;
  padding-inline: var(--modern-space-2);
  text-align: left;
}
.modern-users-head {
  min-height: var(--modern-control-nav);
  border-bottom: var(--modern-line-width) solid var(--modern-border);
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
}
.modern-users-item {
  min-height: calc(var(--modern-space-12) + var(--modern-space-2));
  border-bottom: var(--modern-line-width) solid var(--modern-border);
  padding-block: var(--modern-space-2);
  font-size: var(--modern-font-size-secondary);
}
.modern-users-item:hover {
  background: var(--modern-control-hover);
}
.modern-users-row > div {
  min-width: 0;
}
.modern-users-email :deep(.modern-button) {
  max-width: 100%;
  font-weight: var(--modern-weight-medium);
}
.modern-users-number {
  color: var(--modern-muted);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.modern-users-time {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.modern-users-dialog-body {
  display: grid;
  gap: var(--modern-space-4);
  padding: var(--modern-space-5);
}
.modern-users-dialog-actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--modern-space-2);
}
@media (max-width: 760px) {
  .modern-users-row {
    grid-template-columns:
      minmax(200px, 2fr) 96px 132px 120px minmax(140px, 1fr)
      minmax(140px, 1fr);
    min-width: 900px;
    gap: var(--modern-space-2);
  }
}
</style>
