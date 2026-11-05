<template>
  <fieldset class="gw-row-list">
    <legend class="gw-form-label">{{ label }}</legend>
    <div v-for="(value, i) in modelValue" :key="i" class="gw-row oc-flex oc-flex-middle">
      <oc-text-input
        :id="`gw-${name}-${i}`"
        :model-value="value"
        :type="type"
        :label="`${label} ${i + 1}`"
        class="gw-row-input gw-hide-label"
        :data-testid="`${name}-${i}`"
        @update:model-value="(v: string) => update(i, v)"
      />
      <oc-button
        appearance="raw"
        class="gw-row-remove"
        :aria-label="`${$gettext('Remove')} ${label} ${i + 1}`"
        :data-testid="`${name}-remove-${i}`"
        @click="remove(i)"
      >
        <oc-icon name="close" fill-type="line" />
      </oc-button>
    </div>
    <oc-button
      appearance="raw"
      class="gw-row-add"
      :data-testid="`${name}-add`"
      @click="emit('update:modelValue', [...modelValue, ''])"
    >
      <oc-icon name="add" fill-type="line" />
      {{ addLabel }}
    </oc-button>
  </fieldset>
</template>

<script setup lang="ts">
import { useGettext } from 'vue3-gettext'

defineOptions({ name: 'RowList' })

// A field that can have several values: a text input for each, a button to
// remove a row and one to add a row. The last row is never removed, so there
// is always one to type into.
const props = withDefaults(
  defineProps<{
    name: string
    label: string
    // The text of the button that adds a row, such as "Add email".
    addLabel: string
    modelValue: string[]
    type?: string
  }>(),
  { type: 'text' }
)
const emit = defineEmits<{ 'update:modelValue': [value: string[]] }>()
const { $gettext } = useGettext()

function update(i: number, value: string) {
  const next = [...props.modelValue]
  next[i] = value
  emit('update:modelValue', next)
}

function remove(i: number) {
  const next = props.modelValue.filter((_, j) => j !== i)
  emit('update:modelValue', next.length > 0 ? next : [''])
}
</script>

<style>
.gw-row-list {
  border: 0;
  padding: 0;
  margin: 0 0 var(--oc-space-medium);
}
.gw-form-label {
  padding: 0;
  margin-bottom: var(--oc-space-xsmall);
  font-weight: var(--oc-font-weight-semibold);
}
.gw-row {
  gap: var(--oc-space-xsmall);
  margin-bottom: var(--oc-space-xsmall);
}
.gw-row-input {
  flex: 1;
  min-width: 0;
}
/* The label is in the markup for assistive technology; the legend above the
   rows is what shows. */
.gw-hide-label .oc-label {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
}
</style>
