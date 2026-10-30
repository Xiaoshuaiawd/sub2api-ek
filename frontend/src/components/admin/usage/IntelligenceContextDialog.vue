<template>
  <BaseDialog :show="usageId !== null || contextId !== null" :title="t('admin.usage.intelligenceContext.title')" width="extra-wide" close-on-click-outside @close="$emit('close')">
    <div class="space-y-4">
      <p v-if="loading" class="text-sm text-gray-500" role="status">{{ t('common.loading') }}</p>
      <p v-else-if="error" class="text-sm text-red-600 dark:text-red-400" role="alert">{{ error }}</p>
      <template v-else-if="context">
        <div class="grid gap-3 text-sm sm:grid-cols-2">
          <p><span class="text-gray-500">{{ t('admin.usage.requestId') }}:</span> <span class="break-all">{{ context.request_id || '-' }}</span></p>
          <p><span class="text-gray-500">{{ t('admin.usage.intelligenceContext.contentType') }}:</span> <span class="break-all">{{ context.content_type || '-' }}</span></p>
          <p v-if="context.content_encoding"><span class="text-gray-500">{{ t('admin.usage.intelligenceContext.contentEncoding') }}:</span> <span class="break-all">{{ context.content_encoding }}</span></p>
        </div>
        <section>
          <h3 class="mb-2 text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.usage.intelligenceContext.matchedRules') }}</h3>
          <ul v-if="context.matched_rules?.length" class="space-y-2">
            <li v-for="(rule, index) in context.matched_rules" :key="`${rule.id}-${index}`" class="rounded-md bg-primary-50 px-3 py-2 text-sm text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
              <span class="font-medium">{{ rule.name }}</span>
              <pre v-if="rule.pattern" class="mt-1 whitespace-pre-wrap break-all font-mono text-xs">{{ rule.pattern }}</pre>
            </li>
          </ul>
          <p v-else class="text-sm text-gray-500">{{ t('admin.usage.intelligenceContext.noRules') }}</p>
        </section>
        <section>
          <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
            <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.usage.intelligenceContext.requestBody') }}</h3>
            <button v-if="context.request_body_base64" type="button" data-testid="download-intelligence-context" class="btn btn-secondary btn-sm" @click="downloadBody">{{ t('admin.usage.intelligenceContext.download') }}</button>
          </div>
          <p v-if="context.content_encoding" class="mb-2 text-xs text-gray-500">{{ t('admin.usage.intelligenceContext.encodingHint') }}</p>
          <pre v-if="context.request_body !== null" data-testid="intelligence-context-body" class="max-h-[60vh] overflow-auto whitespace-pre-wrap break-all rounded-lg bg-gray-50 p-4 font-mono text-xs text-gray-700 dark:bg-dark-900 dark:text-gray-300">{{ context.request_body }}</pre>
          <p v-else class="text-sm text-gray-500">{{ t('admin.usage.intelligenceContext.binaryBody') }}</p>
        </section>
      </template>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { getIntelligenceContext, getIntelligenceContextById, type IntelligenceContext } from '@/api/admin/intelligenceContext'

const props = withDefaults(defineProps<{ usageId?: number | null; contextId?: string | null }>(), { usageId: null, contextId: null })
defineEmits<{ close: [] }>()
const { t } = useI18n()
const context = ref<IntelligenceContext | null>(null)
const loading = ref(false)
const error = ref('')

watch(() => [props.usageId, props.contextId] as const, ([usageId, contextId], _old, onCleanup) => {
  context.value = null
  error.value = ''
  loading.value = false
	if (usageId === null && contextId === null) return
  const controller = new AbortController()
  onCleanup(() => controller.abort())
  loading.value = true
	const request = contextId !== null ? getIntelligenceContextById(contextId, controller.signal) : getIntelligenceContext(usageId!, controller.signal)
	request.then(value => {
    if (!controller.signal.aborted) context.value = value
  }).catch((err: { status?: number }) => {
    if (!controller.signal.aborted) error.value = t(err?.status === 404 ? 'admin.usage.intelligenceContext.notFound' : 'admin.usage.intelligenceContext.loadError')
  }).finally(() => {
    if (!controller.signal.aborted) loading.value = false
  })
}, { immediate: true })

function downloadBody() {
  if (!context.value?.request_body_base64) return
  const raw = atob(context.value.request_body_base64)
  const bytes = Uint8Array.from(raw, char => char.charCodeAt(0))
  const url = URL.createObjectURL(new Blob([bytes], { type: 'application/octet-stream' }))
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = `intelligence-request-${context.value.id || props.usageId || props.contextId}.bin`
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
</script>
