<template>
  <div class="space-y-2 rounded-lg border border-gray-200 p-3 dark:border-dark-600" data-testid="condition-node">
    <label v-if="modelValue.op === 'all' || modelValue.op === 'any'" class="block text-sm font-medium text-gray-800 dark:text-gray-100">
      {{ t('admin.settings.intelligenceRules.conditionLogic') }}
      <select data-testid="condition-operator" class="input mt-1 w-full" :value="modelValue.op" @change="updateOperator">
        <option value="all">{{ t('admin.settings.intelligenceRules.conditionOp.all') }}</option>
        <option value="any">{{ t('admin.settings.intelligenceRules.conditionOp.any') }}</option>
      </select>
    </label>
    <p v-else class="text-sm font-medium text-gray-800 dark:text-gray-100">{{ t(`admin.settings.intelligenceRules.conditionOp.${modelValue.op}`) }}</p>
    <template v-if="modelValue.op === 'all' || modelValue.op === 'any' || modelValue.op === 'not'">
      <div v-for="(child, index) in modelValue.children || []" :key="index" class="space-y-2">
        <IntelligenceConditionEditor :model-value="child" @update:model-value="updateChild(index, $event)" />
        <button v-if="modelValue.op !== 'not'" type="button" data-testid="condition-remove" class="btn btn-secondary btn-sm text-red-600 dark:text-red-400" @click="removeChild(index)">{{ t('admin.settings.intelligenceRules.conditionRemove') }}</button>
      </div>
      <div v-if="modelValue.op !== 'not'" class="flex flex-wrap gap-2">
        <button type="button" data-testid="condition-add-contains" class="btn btn-secondary btn-sm" @click="addChild('contains_any')">{{ t('admin.settings.intelligenceRules.conditionAddContains') }}</button>
        <button type="button" data-testid="condition-add-regex" class="btn btn-secondary btn-sm" @click="addChild('regex')">{{ t('admin.settings.intelligenceRules.conditionAddRegex') }}</button>
        <button type="button" data-testid="condition-add-not" class="btn btn-secondary btn-sm" @click="addChild('not')">{{ t('admin.settings.intelligenceRules.conditionAddNot') }}</button>
      </div>
    </template>
    <template v-else>
      <label class="block text-sm text-gray-700 dark:text-gray-300">
        {{ t('admin.settings.intelligenceRules.conditionTextLabel') }}
        <select data-testid="condition-text" class="input mt-1 w-full" :value="modelValue.text || 'plain'" @change="updateText">
          <option value="plain">{{ t('admin.settings.intelligenceRules.conditionText.plain') }}</option>
          <option value="qin">{{ t('admin.settings.intelligenceRules.conditionText.qin') }}</option>
          <option value="visual">{{ t('admin.settings.intelligenceRules.conditionText.visual') }}</option>
        </select>
      </label>
      <label v-if="modelValue.op === 'contains_any'" class="block text-sm text-gray-700 dark:text-gray-300">
        {{ t('admin.settings.intelligenceRules.conditionValues') }}
        <textarea
          data-testid="condition-values"
          class="input mt-1 w-full font-mono"
          :rows="Math.min(12, Math.max(3, (modelValue.values || []).length))"
          :value="(modelValue.values || []).join('\n')"
          @input="updateValues"
        />
      </label>
      <label v-else-if="modelValue.op === 'regex'" class="block text-sm text-gray-700 dark:text-gray-300">
        {{ t('admin.settings.intelligenceRules.conditionRegex') }}
        <textarea
          data-testid="condition-pattern"
          class="input mt-1 w-full font-mono"
          rows="3"
          :value="modelValue.pattern || ''"
          @input="updatePattern"
        />
        <span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.intelligenceRules.conditionRegexHint') }}</span>
      </label>
    </template>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { IntelligenceCondition } from '@/api/admin/intelligenceRules'

defineOptions({ name: 'IntelligenceConditionEditor' })
const props = defineProps<{ modelValue: IntelligenceCondition }>()
const emit = defineEmits<{ 'update:modelValue': [value: IntelligenceCondition] }>()
const { t } = useI18n()

function updateChild(index: number, value: IntelligenceCondition) {
  const children = [...(props.modelValue.children || [])]
  children[index] = value
  emit('update:modelValue', { ...props.modelValue, children })
}
function removeChild(index: number) {
  const children = [...(props.modelValue.children || [])]
  children.splice(index, 1)
  emit('update:modelValue', { ...props.modelValue, children })
}
function addChild(op: 'contains_any' | 'regex' | 'not') {
  const leaf: IntelligenceCondition = { op: 'contains_any', text: 'plain', values: [''] }
  const child: IntelligenceCondition = op === 'not' ? { op, children: [leaf] } : op === 'regex' ? { op, text: 'plain', pattern: '' } : leaf
  emit('update:modelValue', { ...props.modelValue, children: [...(props.modelValue.children || []), child] })
}
function updateOperator(event: Event) {
  const op = (event.target as HTMLSelectElement).value as 'all' | 'any'
  emit('update:modelValue', { ...props.modelValue, op })
}
function updateText(event: Event) {
  const text = (event.target as HTMLSelectElement).value as 'plain' | 'qin' | 'visual'
  emit('update:modelValue', { ...props.modelValue, text })
}
function updateValues(event: Event) {
  emit('update:modelValue', { ...props.modelValue, values: (event.target as HTMLTextAreaElement).value.split(/\r\n|\n|\r/) })
}
function updatePattern(event: Event) {
  emit('update:modelValue', { ...props.modelValue, pattern: (event.target as HTMLTextAreaElement).value })
}
</script>
