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
        <fieldset v-if="builtins.length" :disabled="saving" class="space-y-3">
          <div>
            <h3 class="font-medium text-gray-900 dark:text-white">{{ t('admin.settings.intelligenceRules.builtinTitle') }}</h3>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.intelligenceRules.builtinHint') }}</p>
          </div>
          <div v-for="rule in builtins" :key="rule.id" data-testid="builtin-rule" class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-dark-600">
            <div class="flex flex-wrap items-start justify-between gap-4">
              <div class="min-w-0">
                <span class="block font-medium text-gray-900 dark:text-white">{{ rule.name }}</span>
                <span class="mt-1 block text-sm text-gray-500 dark:text-gray-400">{{ rule.description }}</span>
              </div>
              <label class="inline-flex cursor-pointer items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
                <input v-model="rule.enabled" data-testid="builtin-rule-enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
                {{ t('admin.settings.intelligenceRules.enabled') }}
              </label>
            </div>
            <p v-if="(builtinExtensions[rule.id] || []).length" class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.intelligenceRules.builtinExtensionCount', { count: (builtinExtensions[rule.id] || []).length }) }}</p>
            <button type="button" data-testid="builtin-rule-edit" class="btn btn-secondary btn-sm" :aria-expanded="editingBuiltins.has(rule.id)" :aria-label="`${t(editingBuiltins.has(rule.id) ? 'admin.settings.intelligenceRules.doneEditing' : 'admin.settings.intelligenceRules.edit')} ${rule.name}`" @click="toggleBuiltinEdit(rule.id)">{{ t(editingBuiltins.has(rule.id) ? 'admin.settings.intelligenceRules.doneEditing' : 'admin.settings.intelligenceRules.edit') }}</button>
            <div v-if="editingBuiltins.has(rule.id)" class="space-y-3 border-t border-gray-100 pt-3 dark:border-dark-700">
              <div v-if="conditionFor(rule)" class="space-y-3">
                <div class="flex flex-wrap items-center justify-between gap-2">
                  <div>
                    <h4 class="font-medium text-gray-900 dark:text-white">{{ t('admin.settings.intelligenceRules.originalConditions') }}</h4>
                    <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.intelligenceRules.originalConditionsHint') }}</p>
                  </div>
                  <button type="button" data-testid="builtin-condition-reset" class="btn btn-secondary btn-sm" :disabled="!rule.default_condition" @click="restoreBuiltinCondition(rule.id)">{{ t('admin.settings.intelligenceRules.restoreOriginal') }}</button>
                </div>
                <IntelligenceConditionEditor :model-value="conditionFor(rule)!" @update:model-value="updateBuiltinCondition(rule, $event)" />
              </div>
              <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.intelligenceRules.builtinExtensionHint') }}</p>
              <div v-for="(extension, index) in (builtinExtensions[rule.id] || [])" :key="index" data-testid="builtin-extension" class="space-y-2 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
                <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">
                  {{ t('admin.settings.intelligenceRules.type') }}
                  <select v-model="extension.type" data-testid="builtin-extension-type" class="input mt-1 w-full">
                    <option value="contains">{{ t('admin.settings.intelligenceRules.contains') }}</option>
                    <option value="regex">{{ t('admin.settings.intelligenceRules.regex') }}</option>
                  </select>
                </label>
                <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">
                  {{ t('admin.settings.intelligenceRules.pattern') }}
                  <textarea v-model="extension.pattern" data-testid="builtin-extension-pattern" class="input mt-1 w-full font-mono" rows="2" :placeholder="t('admin.settings.intelligenceRules.patternPlaceholder')" maxlength="4096" />
                </label>
                <button type="button" data-testid="builtin-extension-remove" class="btn btn-secondary btn-sm text-red-600 dark:text-red-400" @click="removeBuiltinExtension(rule.id, index)">{{ t('admin.settings.intelligenceRules.remove') }}</button>
              </div>
              <button type="button" data-testid="builtin-extension-add" class="btn btn-secondary btn-sm" :disabled="totalBuiltinExtensions >= 200" @click="addBuiltinExtension(rule.id)">{{ t('admin.settings.intelligenceRules.builtinExtensionAdd') }}</button>
            </div>
          </div>
        </fieldset>
        <h3 class="border-t border-gray-100 pt-4 font-medium text-gray-900 dark:border-dark-700 dark:text-white">{{ t('admin.settings.intelligenceRules.customTitle') }}</h3>
        <p v-if="rules.length === 0" class="rounded-lg border border-dashed border-gray-300 px-4 py-6 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">{{ t('admin.settings.intelligenceRules.empty') }}</p>
        <fieldset :disabled="saving" class="space-y-3">
          <div v-for="(rule, index) in rules" :key="rule.id" data-testid="intelligence-rule" class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-dark-600">
            <div v-if="!editingCustom.has(rule.id)" class="min-w-0 space-y-1">
              <p class="font-medium text-gray-900 dark:text-white">{{ rule.name }}</p>
              <p class="text-sm text-gray-500 dark:text-gray-400">{{ t(rule.type === 'regex' ? 'admin.settings.intelligenceRules.regex' : 'admin.settings.intelligenceRules.contains') }}</p>
              <p class="whitespace-pre-wrap break-all font-mono text-sm text-gray-700 dark:text-gray-300">{{ rule.pattern }}</p>
            </div>
            <div v-if="editingCustom.has(rule.id)" class="grid gap-3 sm:grid-cols-2">
              <label class="block min-w-0 text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('admin.settings.intelligenceRules.name') }}
                <input :id="`intelligence-rule-name-${rule.id}`" v-model="rule.name" data-testid="rule-name" class="input mt-1 w-full" :placeholder="t('admin.settings.intelligenceRules.namePlaceholder')" maxlength="256" @keydown.enter.prevent />
              </label>
              <label class="block min-w-0 text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('admin.settings.intelligenceRules.type') }}
                <select v-model="rule.type" data-testid="rule-type" class="input mt-1 w-full">
                  <option value="contains">{{ t('admin.settings.intelligenceRules.contains') }}</option>
                  <option value="regex">{{ t('admin.settings.intelligenceRules.regex') }}</option>
                </select>
              </label>
            </div>
            <label v-if="editingCustom.has(rule.id)" class="block text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.settings.intelligenceRules.pattern') }}
              <textarea v-model="rule.pattern" data-testid="rule-pattern" class="input mt-1 w-full font-mono" rows="2" :placeholder="t('admin.settings.intelligenceRules.patternPlaceholder')" maxlength="4096" />
            </label>
            <div class="flex flex-wrap items-center justify-between gap-3">
              <label class="inline-flex cursor-pointer items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
                <input v-model="rule.enabled" data-testid="rule-enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
                {{ t('admin.settings.intelligenceRules.enabled') }}
              </label>
              <div class="flex flex-wrap gap-2">
                <button type="button" data-testid="rule-edit" class="btn btn-secondary btn-sm" :aria-expanded="editingCustom.has(rule.id)" :aria-label="`${t(editingCustom.has(rule.id) ? 'admin.settings.intelligenceRules.doneEditing' : 'admin.settings.intelligenceRules.edit')} ${rule.name}`" @click="toggleCustomEdit(rule.id)">{{ t(editingCustom.has(rule.id) ? 'admin.settings.intelligenceRules.doneEditing' : 'admin.settings.intelligenceRules.edit') }}</button>
                <button type="button" data-testid="rule-remove" class="btn btn-secondary btn-sm text-red-600 dark:text-red-400" @click="removeCustom(index)">{{ t('admin.settings.intelligenceRules.remove') }}</button>
              </div>
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
import { computed, nextTick, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getRules, saveRules, type IntelligenceRule, type IntelligenceBuiltinRule, type IntelligenceBuiltinExtension, type IntelligenceCondition } from '@/api/admin/intelligenceRules'
import IntelligenceConditionEditor from './IntelligenceConditionEditor.vue'

type EditableRule = IntelligenceRule & { id: number }
const { t } = useI18n()
const rules = ref<EditableRule[]>([])
const builtins = ref<IntelligenceBuiltinRule[]>([])
const builtinExtensions = ref<Record<string, IntelligenceBuiltinExtension[]>>({})
const builtinConditions = ref<Record<string, IntelligenceCondition>>({})
const conditionsSupported = ref(false)
const editingBuiltins = ref(new Set<string>())
const editingCustom = ref(new Set<number>())
const previouslyDisabled = ref<string[]>([])
const loading = ref(false)
const loaded = ref(false)
const saving = ref(false)
const error = ref('')
const success = ref('')
const savedSnapshot = ref('')
let nextId = 0
const editable = (values: IntelligenceRule[]) => values.map(rule => ({ ...rule, id: nextId++ }))
const payload = () => {
  const builtinIds = new Set(builtins.value.map(rule => rule.id))
  const unknownDisabled = previouslyDisabled.value.filter(id => !builtinIds.has(id))
  const data = {
    rules: rules.value.map(({ name, type, pattern, enabled }) => ({ name, type, pattern, enabled })),
    builtin_disabled_ids: [...unknownDisabled, ...builtins.value.filter(rule => !rule.enabled).map(rule => rule.id)],
    builtin_extensions: Object.fromEntries(Object.entries(builtinExtensions.value).map(([id, extensions]) => [id, extensions.map(({ type, pattern }) => ({ type, pattern }))])),
  }
  return conditionsSupported.value ? { ...data, builtin_conditions: builtinConditions.value } : data
}
const dirty = computed(() => JSON.stringify(payload()) !== savedSnapshot.value)
const totalBuiltinExtensions = computed(() => Object.values(builtinExtensions.value).reduce((sum, extensions) => sum + extensions.length, 0))
const message = (value: unknown) => (value as { message?: string })?.message || t('admin.settings.intelligenceRules.error')

function add() {
  const id = nextId++
  rules.value.push({ id, name: '', type: 'contains', pattern: '', enabled: true })
  editingCustom.value.add(id)
  void nextTick(() => document.getElementById(`intelligence-rule-name-${id}`)?.focus())
  success.value = ''
}
function toggleCustomEdit(id: number) {
  if (editingCustom.value.has(id)) editingCustom.value.delete(id)
  else {
    editingCustom.value.add(id)
    void nextTick(() => document.getElementById(`intelligence-rule-name-${id}`)?.focus())
  }
}
function removeCustom(index: number) {
  const rule = rules.value[index]
  if (rule) editingCustom.value.delete(rule.id)
  rules.value.splice(index, 1)
}
function toggleBuiltinEdit(id: string) {
  if (editingBuiltins.value.has(id)) editingBuiltins.value.delete(id)
  else editingBuiltins.value.add(id)
}
function addBuiltinExtension(id: string) {
  if (!builtinExtensions.value[id]) builtinExtensions.value[id] = []
  builtinExtensions.value[id].push({ type: 'contains', pattern: '' })
}
function removeBuiltinExtension(id: string, index: number) {
  const extensions = builtinExtensions.value[id]
  if (!extensions) return
  extensions.splice(index, 1)
  if (extensions.length === 0) delete builtinExtensions.value[id]
}
function conditionFor(rule: IntelligenceBuiltinRule): IntelligenceCondition | undefined {
  return builtinConditions.value[rule.id] || rule.default_condition || rule.condition
}
function updateBuiltinCondition(rule: IntelligenceBuiltinRule, value: IntelligenceCondition) {
  if (rule.default_condition && JSON.stringify(value) === JSON.stringify(rule.default_condition)) delete builtinConditions.value[rule.id]
  else builtinConditions.value[rule.id] = value
}
function restoreBuiltinCondition(id: string) {
  delete builtinConditions.value[id]
}
function validBuiltinCondition(root: IntelligenceCondition): boolean {
  let count = 0
  const validate = (node: IntelligenceCondition, depth: number): boolean => {
    count++
    if (depth > 16 || count > 256) return false
    if (node.op === 'all' || node.op === 'any' || node.op === 'not') {
      const children = node.children || []
      return children.length > 0 && children.length <= 100 && (node.op !== 'not' || children.length === 1) && children.every(child => validate(child, depth + 1))
    }
    if (node.op === 'contains_any') {
      const values = node.values || []
      return values.length > 0 && values.length <= 200 && values.every(value => value !== '' && new TextEncoder().encode(value).length <= 4096)
    }
    return node.op === 'regex' && !!node.pattern && new TextEncoder().encode(node.pattern).length <= 4096
  }
  return validate(root, 0)
}
async function load() {
  loading.value = true
  error.value = ''
  try {
    const result = await getRules()
    rules.value = editable(result.rules || [])
    builtins.value = result.builtins || []
    previouslyDisabled.value = result.builtin_disabled_ids || []
    builtinExtensions.value = result.builtin_extensions || {}
    builtinConditions.value = result.builtin_conditions || {}
    conditionsSupported.value = 'builtin_conditions' in result || (result.builtins || []).some(rule => !!rule.condition)
    editingBuiltins.value.clear()
    editingCustom.value.clear()
    savedSnapshot.value = JSON.stringify(payload())
    loaded.value = true
  } catch (err) { error.value = message(err) }
  finally { loading.value = false }
}
async function save() {
  if (Object.values(builtinConditions.value).some(condition => !validBuiltinCondition(condition))) {
    error.value = t('admin.settings.intelligenceRules.conditionInvalid')
    return
  }
  if (rules.value.some(rule => !rule.name.trim() || !rule.pattern.trim())) {
    error.value = t('admin.settings.intelligenceRules.required')
    return
  }
  const extensions = Object.values(builtinExtensions.value).flat()
  if (extensions.length > 200) {
    error.value = t('admin.settings.intelligenceRules.extensionLimit')
    return
  }
  if (extensions.some(extension => !extension.pattern.trim())) {
    error.value = t('admin.settings.intelligenceRules.extensionRequired')
    return
  }
  if (extensions.some(extension => new TextEncoder().encode(extension.pattern).length > 4096)) {
    error.value = t('admin.settings.intelligenceRules.tooLong')
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
    const result = await saveRules(payload())
    rules.value = editable(result.rules || [])
    builtins.value = result.builtins || []
    previouslyDisabled.value = result.builtin_disabled_ids || []
    builtinExtensions.value = result.builtin_extensions || {}
    builtinConditions.value = result.builtin_conditions || {}
    conditionsSupported.value = 'builtin_conditions' in result || (result.builtins || []).some(rule => !!rule.condition)
    editingBuiltins.value.clear()
    editingCustom.value.clear()
    savedSnapshot.value = JSON.stringify(payload())
    success.value = t('admin.settings.intelligenceRules.saved')
  } catch (err) { error.value = message(err) }
  finally { saving.value = false }
}
onMounted(load)
</script>
