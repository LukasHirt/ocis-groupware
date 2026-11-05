<template>
  <fieldset class="row-list">
    <legend class="oc-text-bold">{{ label }}</legend>
    <div v-for="(value, i) in modelValue" :key="i" class="oc-flex oc-flex-middle oc-mb-xs">
      <input
        :value="value"
        :type="type"
        class="oc-text-input oc-mr-s"
        :aria-label="`${label} ${i + 1}`"
        :data-testid="`${name}-${i}`"
        @input="update(i, ($event.target as HTMLInputElement).value)"
      />
      <button
        type="button"
        class="oc-button oc-button-s oc-button-passive"
        :aria-label="`${$gettext('Remove')} ${label} ${i + 1}`"
        @click="remove(i)"
      >
        {{ $gettext('Remove') }}
      </button>
    </div>
    <button
      type="button"
      class="oc-button oc-button-s oc-button-passive"
      :data-testid="`${name}-add`"
      @click="emit('update:modelValue', [...modelValue, ''])"
    >
      {{ $gettext('Add') }}
    </button>
  </fieldset>
</template>

<script setup lang="ts">
import { useGettext } from 'vue3-gettext'

defineOptions({ name: 'RowList' })

const props = withDefaults(
  defineProps<{ name: string; label: string; modelValue: string[]; type?: string }>(),
  { type: 'text' }
)
const emit = defineEmits<{ 'update:modelValue': [value: string[]] }>()
const { $gettext } = useGettext()

function update(i: number, value: string) {
  const next = [...props.modelValue]
  next[i] = value
  emit('update:modelValue', next)
}

// The last row is never removed, so there is always one to type into.
function remove(i: number) {
  const next = props.modelValue.filter((_, j) => j !== i)
  emit('update:modelValue', next.length > 0 ? next : [''])
}
</script>

<style scoped>
.row-list {
  border: 0;
  padding: 0;
  margin: 0 0 var(--oc-space-medium);
}
.row-list input {
  flex: 1;
}
</style>
