<template>
  <div>
    <div class="flex items-center justify-between gap-3 px-4 py-4">
      <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.usage.intelligenceContext.listDescription') }}</p>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="load">{{ t('admin.usage.intelligenceContext.refresh') }}</button>
    </div>
    <p v-if="error" role="alert" class="px-4 pb-4 text-sm text-red-600 dark:text-red-400">{{ error }}</p>
    <p v-else-if="loading" role="status" class="px-4 pb-4 text-sm text-gray-500">{{ t('common.loading') }}</p>
    <p v-else-if="items.length === 0" class="px-4 pb-6 text-sm text-gray-500">{{ t('admin.usage.intelligenceContext.empty') }}</p>
    <div v-else class="overflow-x-auto">
      <table class="w-full text-left text-sm">
        <thead class="bg-gray-50 text-gray-600 dark:bg-dark-800 dark:text-gray-300"><tr>
          <th class="px-4 py-3">{{ t('usage.time') }}</th>
          <th class="px-4 py-3">{{ t('admin.usage.requestId') }}</th>
          <th class="px-4 py-3">{{ t('admin.usage.intelligenceContext.matchedRules') }}</th>
          <th class="px-4 py-3">{{ t('admin.usage.intelligenceContext.usageLog') }}</th>
          <th class="px-4 py-3"><span class="sr-only">{{ t('admin.usage.intelligenceContext.view') }}</span></th>
        </tr></thead>
        <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
          <tr v-for="item in items" :key="item.id">
            <td class="whitespace-nowrap px-4 py-3">{{ formatDateTime(item.created_at) }}</td>
            <td class="max-w-[240px] break-all px-4 py-3 font-mono text-xs">{{ item.request_id || '-' }}</td>
            <td class="px-4 py-3">{{ (item.matched_rules || []).map(rule => rule.name).join('、') || '-' }}</td>
            <td class="px-4 py-3">{{ item.usage_log_id ? `#${item.usage_log_id}` : t('admin.usage.intelligenceContext.noUsageLog') }}</td>
            <td class="px-4 py-3"><button type="button" data-testid="open-intelligence-context" class="font-medium text-primary-600 hover:underline dark:text-primary-400" @click="$emit('open', item.id)">{{ t('admin.usage.intelligenceContext.view') }}</button></td>
          </tr>
        </tbody>
      </table>
    </div>
    <Pagination v-if="total > 0" :page="page" :page-size="pageSize" :total="total" @update:page="changePage" @update:pageSize="changePageSize" />
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Pagination from '@/components/common/Pagination.vue'
import { formatDateTime } from '@/utils/format'
import { listIntelligenceContexts, type IntelligenceContextSummary } from '@/api/admin/intelligenceContext'

defineEmits<{ open: [id: string] }>()
const { t } = useI18n()
const items = ref<IntelligenceContextSummary[]>([])
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const loading = ref(false)
const error = ref('')
let controller: AbortController | null = null

async function load() {
  controller?.abort()
  const current = new AbortController()
  controller = current
  loading.value = true
  error.value = ''
  try {
    const result = await listIntelligenceContexts(page.value, pageSize.value, current.signal)
    if (!current.signal.aborted) { items.value = result.items || []; total.value = result.total }
  } catch {
    if (!current.signal.aborted) error.value = t('admin.usage.intelligenceContext.loadError')
  } finally {
    if (!current.signal.aborted) loading.value = false
  }
}
function changePage(value: number) { page.value = value; void load() }
function changePageSize(value: number) { pageSize.value = value; page.value = 1; void load() }
onMounted(load)
onUnmounted(() => controller?.abort())
</script>
