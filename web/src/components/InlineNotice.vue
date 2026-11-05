<template>
  <div class="gw-notice" :class="`gw-notice-${variation}`" :role="role">
    <oc-icon :name="icon" fill-type="line" size="small" :variation="iconVariation" class="gw-notice-icon" />
    <div class="gw-notice-text"><slot /></div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

defineOptions({ name: 'InlineNotice' })

// A short note inside a page, in the manner of the notices in oCIS Web: an
// icon, a tinted background and text. A warning or an error is announced as
// soon as it appears.
const props = withDefaults(
  defineProps<{ variation?: 'info' | 'warning' | 'danger' }>(),
  { variation: 'info' }
)

const icon = computed(() => (props.variation === 'info' ? 'information' : 'error-warning'))
const iconVariation = computed(() => (props.variation === 'info' ? 'passive' : props.variation))
const role = computed(() => (props.variation === 'info' ? undefined : 'alert'))
</script>

<style>
.gw-notice {
  display: flex;
  align-items: flex-start;
  gap: var(--oc-space-small);
  padding: var(--oc-space-small) var(--oc-space-medium);
  border-radius: var(--oc-space-xsmall);
  border-left: var(--oc-space-xsmall) solid var(--oc-color-border);
  background: var(--oc-color-background-highlight);
  color: var(--oc-color-text-default);
}
.gw-notice-icon {
  flex: none;
}
.gw-notice-text {
  min-width: 0;
  overflow-wrap: anywhere;
}
.gw-notice-info {
  color: var(--oc-color-text-muted);
}
.gw-notice-warning {
  border-left-color: var(--oc-color-swatch-warning-default);
}
.gw-notice-danger {
  border-left-color: var(--oc-color-swatch-danger-default);
}
</style>
