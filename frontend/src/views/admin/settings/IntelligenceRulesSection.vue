<template>
  <section class="card" data-testid="intelligence-rules-settings">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 id="settings-section-intelligence-rules" tabindex="-1" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.settings.intelligenceRules.title') }}</h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.intelligenceRules.description') }}</p>
      <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.intelligenceRules.hint') }}</p>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.intelligenceRules.limits') }}</p>
    </div>
    <div class="space-y-4 p-6">
      <p v-if="loading" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
      <template v-else-if="!loaded">
        <p class="text-sm text-red-600 dark:text-red-400" role="alert">{{ error }}</p>
        <button type="button" class="btn btn-secondary btn-sm" @click="load">{{ t('admin.settings.intelligenceRules.retry') }}</button>
      </template>
      <template v-else>
        <p v-if="rules.length === 0" class="rounded-lg border border-dashed border-gray-300 px-4 py-6 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">{{ t('admin.settings.intelligenceRules.empty') }}</p>
        <fieldset :disabled="saving" class="space-y-3">
          <div v-for="(rule, index) in rules" :key="rule.id" data-testid="intelligence-rule" class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-dark-600">
            <div class="grid gap-3 sm:grid-cols-2">
              <label class="block min-w-0 text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('admin.settings.intelligenceRules.name') }}
                <input v-model="rule.name" data-testid="rule-name" class="input mt-1 w-full" :placeholder="t('admin.settings.intelligenceRules.namePlaceholder')" maxlength="256" @keydown.enter.prevent />
              </label>
              <label class="block min-w-0 text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('admin.settings.intelligenceRules.type') }}
                <select v-model="rule.type" data-testid="rule-type" class="input mt-1 w-full">
                  <option value="contains">{{ t('admin.settings.intelligenceRules.contains') }}</option>
                  <option value="regex">{{ t('admin.settings.intelligenceRules.regex') }}</option>
                </select>
              </label>
            </div>
            <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.settings.intelligenceRules.pattern') }}
              <textarea v-model="rule.pattern" data-testid="rule-pattern" class="input mt-1 w-full font-mono" rows="2" :placeholder="t('admin.settings.intelligenceRules.patternPlaceholder')" maxlength="4096" />
            </label>
            <div class="flex flex-wrap items-center justify-between gap-3">
              <label class="inline-flex cursor-pointer items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
                <input v-model="rule.enabled" data-testid="rule-enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
                {{ t('admin.settings.intelligenceRules.enabled') }}
              </label>
              <button type="button" data-testid="rule-remove" class="btn btn-secondary btn-sm text-red-600 dark:text-red-400" @click="rules.splice(index, 1)">{{ t('admin.settings.intelligenceRules.remove') }}</button>
            </div>
          </div>
          <button type="button" data-testid="intelligence-add" class="btn btn-secondary btn-sm" :disabled="rules.length >= 200" @click="add">{{ t('admin.settings.intelligenceRules.add') }}</button>
        </fieldset>
        <p v-if="error" class="text-sm text-red-600 dark:text-red-400" role="alert">{{ error }}</p>
        <p v-if="success && !dirty" class="text-sm text-green-700 dark:text-green-400" role="status">{{ success }}</p>
        <p v-if="dirty" class="text-sm text-amber-700 dark:text-amber-400">{{ t('admin.settings.intelligenceRules.unsaved') }}</p>
        <div class="border-t border-gray-100 pt-4 dark:border-dark-700">
          <button type="button" data-testid="intelligence-save" class="btn btn-primary" :disabled="saving" @click="save">{{ saving ? t('admin.settings.intelligenceRules.saving') : t('admin.settings.intelligenceRules.save') }}</button>
        </div>
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getRules, saveRules, type IntelligenceRule } from '@/api/admin/intelligenceRules'

type EditableRule = IntelligenceRule & { id: number }
const { t } = useI18n()
const rules = ref<EditableRule[]>([])
const loading = ref(false)
const loaded = ref(false)
const saving = ref(false)
const error = ref('')
const success = ref('')
const savedSnapshot = ref('')
let nextId = 0
const editable = (values: IntelligenceRule[]) => values.map(rule => ({ ...rule, id: nextId++ }))
const payload = () => rules.value.map(({ name, type, pattern, enabled }) => ({ name, type, pattern, enabled }))
const dirty = computed(() => JSON.stringify(payload()) !== savedSnapshot.value)
const message = (value: unknown) => (value as { message?: string })?.message || t('admin.settings.intelligenceRules.error')

function add() {
  rules.value.push({ id: nextId++, name: '', type: 'contains', pattern: '', enabled: true })
  success.value = ''
}
async function load() {
  loading.value = true
  error.value = ''
  try {
    const result = await getRules()
    rules.value = editable(result.rules || [])
    savedSnapshot.value = JSON.stringify(payload())
    loaded.value = true
  } catch (err) { error.value = message(err) }
  finally { loading.value = false }
}
async function save() {
  if (rules.value.some(rule => !rule.name.trim() || !rule.pattern.trim())) {
    error.value = t('admin.settings.intelligenceRules.required')
    return
  }
  if (rules.value.some(rule => new TextEncoder().encode(rule.pattern).length > 4096)) {
    error.value = t('admin.settings.intelligenceRules.tooLong')
    return
  }
  if (rules.value.some(rule => new TextEncoder().encode(rule.name).length > 256)) {
    error.value = t('admin.settings.intelligenceRules.nameTooLong')
    return
  }
  saving.value = true
  error.value = ''
  success.value = ''
  try {
    const result = await saveRules({ rules: payload() })
    rules.value = editable(result.rules || [])
    savedSnapshot.value = JSON.stringify(payload())
    success.value = t('admin.settings.intelligenceRules.saved')
  } catch (err) { error.value = message(err) }
  finally { saving.value = false }
}
onMounted(load)
</script>
