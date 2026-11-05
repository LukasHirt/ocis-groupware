<template>
  <div ref="backdrop" class="groupware-modal-backdrop" data-testid="modal-backdrop">
    <div
      ref="dialog"
      class="groupware-modal oc-p-m"
      :class="{ 'groupware-modal-wide': wide }"
      role="dialog"
      aria-modal="true"
      :aria-labelledby="titleId"
    >
      <h2 :id="titleId" class="groupware-modal-title oc-mt-rm oc-mb-m">{{ title }}</h2>
      <slot />
    </div>
  </div>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, useId } from 'vue'

defineOptions({ name: 'ModalDialog' })

defineProps<{ title: string; wide?: boolean }>()
const emit = defineEmits<{ close: [] }>()

const titleId = `groupware-modal-title-${useId()}`
const dialog = ref<HTMLElement>()
const backdrop = ref<HTMLElement>()
let opener: Element | null = null

const focusable = 'a[href], button:not([disabled]), input:not([disabled]), select, textarea, [tabindex]:not([tabindex="-1"])'

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    event.stopPropagation()
    emit('close')
    return
  }
  if (event.key !== 'Tab' || !dialog.value) {
    return
  }
  // Keep the focus inside the dialog while it is open.
  const items = [...dialog.value.querySelectorAll<HTMLElement>(focusable)]
  if (items.length === 0) {
    return
  }
  const first = items[0]
  const last = items[items.length - 1]
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault()
    first.focus()
  }
}

// The listeners are added here because the backdrop and the dialog are plain
// containers, not controls; a click outside and the keyboard only close it.
function onBackdropMousedown(event: MouseEvent) {
  if (event.target === backdrop.value) {
    emit('close')
  }
}

onMounted(async () => {
  backdrop.value?.addEventListener('mousedown', onBackdropMousedown)
  dialog.value?.addEventListener('keydown', onKeydown)
  opener = document.activeElement
  await nextTick()
  dialog.value?.querySelector<HTMLElement>('[autofocus], input, button')?.focus()
})

onBeforeUnmount(() => {
  backdrop.value?.removeEventListener('mousedown', onBackdropMousedown)
  dialog.value?.removeEventListener('keydown', onKeydown)
  if (opener instanceof HTMLElement) {
    opener.focus()
  }
})
</script>

<style>
.groupware-modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 1000;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.5);
}
.groupware-modal {
  background: var(--oc-color-background-default);
  color: var(--oc-color-text-default);
  border-radius: 0.5rem;
  width: min(32rem, calc(100vw - 2rem));
  max-height: calc(100vh - 2rem);
  overflow: auto;
  box-shadow: 0 0.5rem 2rem rgba(0, 0, 0, 0.3);
}
.groupware-modal-wide {
  width: min(42rem, calc(100vw - 2rem));
}
.groupware-modal-title {
  font-size: 1.25rem;
}
.groupware-modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 0.5rem;
  margin-top: 1rem;
}
</style>
