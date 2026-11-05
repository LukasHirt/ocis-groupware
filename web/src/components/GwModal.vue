<template>
  <oc-modal
    :title="title"
    :element-class="wide ? 'gw-modal gw-modal-wide' : 'gw-modal'"
    :variation="variation"
    :icon="icon"
    :focus-trap-initial="initialFocus"
    hide-actions
    @cancel="onCancel"
  >
    <template #content>
      <component :is="form ? 'form' : 'div'" novalidate class="gw-modal-form" @submit.prevent="emit('submit')">
        <slot />
        <div class="oc-modal-body-actions oc-flex oc-flex-between oc-flex-middle gw-modal-actions">
          <div class="oc-flex oc-flex-middle gw-modal-gap"><slot name="start" /></div>
          <div class="oc-flex oc-flex-middle gw-modal-gap"><slot name="actions" /></div>
        </div>
      </component>
    </template>
  </oc-modal>
</template>

<script setup lang="ts">
defineOptions({ name: 'GwModal' })

// The dialog of the design system with an action row of our own, so that each
// button keeps its test id, a form can submit from it and a dialog can have a
// third button. Escape and a click on the backdrop emit close, unless the
// dialog is persistent.
const props = withDefaults(
  defineProps<{
    title: string
    // A wide dialog, for forms with several columns.
    wide?: boolean
    variation?: 'passive' | 'primary' | 'danger' | 'success' | 'warning' | 'info'
    icon?: string
    // A selector for the element that takes the focus when the dialog opens.
    // Without it the dialog itself does, which keeps Escape working.
    initialFocus?: string
    // Wraps the content and the actions in a form: a button with
    // submit="submit" submits it, which emits submit.
    form?: boolean
    // Escape and the backdrop do nothing. For a dialog that shows something
    // that cannot be shown again.
    persistent?: boolean
  }>(),
  { variation: 'passive', icon: undefined, initialFocus: undefined }
)
const emit = defineEmits<{ close: []; submit: [] }>()

function onCancel() {
  if (!props.persistent) {
    emit('close')
  }
}
</script>

<style>
.gw-modal-wide {
  max-width: 40rem;
  width: calc(100vw - 2 * var(--oc-space-medium));
}
.gw-modal-form {
  min-width: 0;
}
/* A long form scrolls with the dialog body; the actions stay in view. */
.gw-modal-actions {
  position: sticky;
  bottom: 0;
  background: var(--oc-color-background-default);
  padding-top: var(--oc-space-small);
  margin-top: var(--oc-space-small);
}
.gw-modal-gap {
  gap: var(--oc-space-small);
}
</style>
