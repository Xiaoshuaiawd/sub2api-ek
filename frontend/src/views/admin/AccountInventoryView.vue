<template>
  <AppLayout>
    <div class="mx-auto max-w-7xl space-y-6">
      <header class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <router-link to="/admin/accounts" class="text-sm text-primary-600 hover:underline dark:text-primary-400">{{ t('accountInventory.back') }}</router-link>
          <h1 class="mt-2 text-2xl font-semibold text-gray-900 dark:text-gray-100">{{ t('accountInventory.title') }}</h1>
          <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">{{ t('accountInventory.description') }}</p>
        </div>
        <button type="button" class="btn btn-secondary" :disabled="loading" @click="refresh">{{ t('accountInventory.refresh') }}</button>
      </header>

      <p v-if="error" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ error }}</p>
      <p v-if="notice" role="status" class="rounded-lg bg-emerald-50 p-3 text-sm text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300">{{ notice }}</p>

      <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <div v-for="card in cards" :key="card.key" class="rounded-xl border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-900">
          <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t(`accountInventory.${card.key}`) }}</p>
          <p class="mt-2 text-2xl font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ card.value }}</p>
        </div>
      </div>

      <div class="grid gap-6 xl:grid-cols-2">
        <section class="rounded-xl border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-900">
          <h2 class="text-base font-semibold">{{ t('accountInventory.targetLabel') }}</h2>
          <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-gray-400">{{ t('accountInventory.targetHint') }}</p>
          <div class="mt-5 flex flex-wrap items-end gap-4">
            <label class="block text-sm font-medium">
              <span>{{ t('accountInventory.targetLabel') }}</span>
              <input v-model.number="target" data-testid="inventory-target" class="input mt-2 w-40" type="number" min="0" max="10000" step="1" @input="configDirty = true" />
            </label>
            <label class="flex min-h-11 items-center gap-2 text-sm">
              <input v-model="enabled" data-testid="inventory-enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600" @change="configDirty = true" />
              {{ t('accountInventory.enabled') }}
            </label>
          </div>
          <button type="button" data-testid="inventory-save" class="btn btn-primary mt-5" :disabled="saving || !validTarget" @click="save">
            {{ t(saving ? 'accountInventory.saving' : 'accountInventory.save') }}
          </button>
        </section>

        <section class="rounded-xl border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-900">
          <h2 class="text-base font-semibold">{{ t('accountInventory.importTitle') }}</h2>
          <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-gray-400">{{ t('accountInventory.importHint') }}</p>
          <router-link to="/admin/token-guard-v2" class="mt-2 inline-block text-sm text-primary-600 hover:underline dark:text-primary-400">{{ t('accountInventory.encryptionSetup') }}</router-link>
          <label class="mt-4 block text-sm font-medium">
            <span>{{ t('accountInventory.file') }}</span>
            <input ref="fileInput" type="file" accept=".txt,text/plain" class="mt-2 block w-full text-sm" @change="selectFile" />
          </label>
          <p v-if="selectedFile" class="mt-2 text-xs text-gray-500">{{ t('accountInventory.fileSelected', { name: selectedFile.name }) }}</p>
          <label class="mt-4 block text-sm font-medium">
            <span>{{ t('accountInventory.content') }}</span>
            <textarea v-model="content" data-testid="inventory-content" class="input mt-2 w-full font-mono text-xs" rows="5" autocomplete="off" autocapitalize="off" :spellcheck="false" />
          </label>
          <button type="button" data-testid="inventory-import" class="btn btn-primary mt-4" :disabled="importing" @click="importInventory">
            {{ t(importing ? 'accountInventory.importing' : 'accountInventory.import') }}
          </button>
        </section>
      </div>

      <section class="overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900">
        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 p-5 dark:border-dark-700">
          <h2 class="text-base font-semibold">{{ t('accountInventory.listTitle') }}</h2>
          <p class="text-xs text-gray-500">{{ t('accountInventory.page', { page, total }) }}</p>
        </div>
        <div class="overflow-x-auto">
          <table class="w-full min-w-[650px] text-left text-sm">
            <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-800"><tr>
              <th class="px-5 py-3">{{ t('accountInventory.email') }}</th>
              <th class="px-4 py-3">{{ t('accountInventory.state') }}</th>
              <th class="px-4 py-3">{{ t('accountInventory.accountId') }}</th>
              <th class="px-4 py-3">{{ t('accountInventory.attempts') }}</th>
              <th class="px-5 py-3">{{ t('accountInventory.reason') }}</th>
            </tr></thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="row in rows" :key="row.id">
                <td class="px-5 py-3 font-mono">{{ row.email }}</td>
                <td class="px-4 py-3">{{ t(`accountInventory.states.${row.state}`) }}</td>
                <td class="px-4 py-3">{{ row.account_id ?? '—' }}</td>
                <td class="px-4 py-3">{{ row.attempts }}</td>
                <td class="px-5 py-3 font-mono text-xs text-gray-500">{{ row.error_code || '—' }}</td>
              </tr>
              <tr v-if="!rows.length"><td colspan="5" class="px-5 py-12 text-center text-gray-500">{{ t('accountInventory.empty') }}</td></tr>
            </tbody>
          </table>
        </div>
        <div class="flex justify-end gap-3 border-t border-gray-100 p-4 dark:border-dark-700">
          <button type="button" class="btn btn-secondary" :disabled="page <= 1 || loading" @click="changePage(page - 1)">{{ t('accountInventory.previous') }}</button>
          <button type="button" class="btn btn-secondary" :disabled="page * 50 >= total || loading" @click="changePage(page + 1)">{{ t('accountInventory.next') }}</button>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { getStatus, saveConfig, importLines, listRows } from '@/api/admin/accountInventory'
import type { AccountInventoryRow, AccountInventoryStatus } from '@/api/admin/accountInventory'

const { t } = useI18n()
const status = ref<AccountInventoryStatus | null>(null)
const rows = ref<AccountInventoryRow[]>([])
const total = ref(0)
const page = ref(1)
const target = ref(0)
const enabled = ref(false)
const content = ref('')
const selectedFile = ref<File | null>(null)
const fileInput = ref<HTMLInputElement | null>(null)
const loading = ref(false)
const saving = ref(false)
const importing = ref(false)
const error = ref('')
const notice = ref('')
const validTarget = computed(() => Number.isInteger(target.value) && target.value >= 0 && target.value <= 10000)
let configLoaded = false
const configDirty = ref(false)
const cards = computed(() => [
  { key: 'available', value: status.value?.available ?? '—' },
  { key: 'target', value: status.value?.target_count ?? '—' },
  { key: 'pending', value: status.value?.pending ?? '—' },
  { key: 'activating', value: status.value?.activating ?? '—' },
  { key: 'failed', value: status.value?.failed ?? '—' },
  { key: 'active', value: status.value?.active ?? '—' },
  { key: 'retired', value: status.value?.retired ?? '—' },
  { key: 'duplicate', value: status.value?.duplicate ?? '—' },
])
let timer: ReturnType<typeof setInterval> | undefined

async function refresh() {
  if (loading.value) return
  loading.value = true
  try {
    const [nextStatus, nextPage] = await Promise.all([getStatus(), listRows(page.value, 50)])
    status.value = nextStatus
    rows.value = nextPage.items
    total.value = nextPage.total
    if (!configLoaded || !configDirty.value) {
      target.value = nextStatus.target_count
      enabled.value = nextStatus.enabled
      configLoaded = true
    }
    error.value = ''
  } catch {
    error.value = t('accountInventory.loadFailed')
  } finally {
    loading.value = false
  }
}

async function save() {
  if (!validTarget.value || saving.value) return
  saving.value = true
  error.value = ''
  notice.value = ''
  try {
    status.value = await saveConfig(target.value, enabled.value)
    configDirty.value = false
    notice.value = t('accountInventory.saved')
  } catch {
    error.value = t('accountInventory.saveFailed')
  } finally {
    saving.value = false
  }
}

function selectFile(event: Event) {
  selectedFile.value = (event.target as HTMLInputElement).files?.[0] ?? null
}

async function importInventory() {
  if (importing.value) return
  importing.value = true
  error.value = ''
  notice.value = ''
  try {
    if (selectedFile.value && selectedFile.value.size > 8 * 1024 * 1024) {
      error.value = t('accountInventory.fileTooLarge')
      return
    }
    const raw = selectedFile.value ? await selectedFile.value.text() : content.value
    if (!raw.trim()) { error.value = t('accountInventory.emptyInput'); return }
    const result = await importLines(raw)
    content.value = ''
    selectedFile.value = null
    if (fileInput.value) fileInput.value.value = ''
    notice.value = t('accountInventory.importResult', { imported: result.imported, skipped: result.skipped })
    page.value = 1
    await refresh()
  } catch (cause: unknown) {
    const detail = (cause as { response?: { data?: { message?: string } } })?.response?.data?.message
    const line = typeof detail === 'string' ? /^第 (\d+) 行/.exec(detail)?.[1] : undefined
    error.value = line ? t('accountInventory.invalidLine', { line }) : t('accountInventory.importFailed')
  } finally {
    importing.value = false
  }
}

async function changePage(next: number) {
  page.value = next
  await refresh()
}

onMounted(() => {
  void refresh()
  timer = setInterval(() => { void refresh() }, 15000)
})
onBeforeUnmount(() => { if (timer) clearInterval(timer) })
</script>
