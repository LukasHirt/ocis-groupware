<template>
  <main class="oc-p-m groupware-page">
    <h1 class="oc-page-title">{{ title }}</h1>
    <div v-if="loading" class="oc-flex oc-flex-middle oc-mt-m" role="status">
      <oc-spinner :aria-label="$gettext('Loading')" />
    </div>
    <p v-else-if="empty" class="oc-text-muted oc-mt-m" data-testid="empty">{{ emptyText }}</p>
    <slot v-else />
  </main>
</template>

<script setup lang="ts">
defineOptions({ name: 'PageLayout' })

withDefaults(
  defineProps<{
    title: string
    loading?: boolean
    empty?: boolean
    emptyText?: string
  }>(),
  { loading: false, empty: false, emptyText: '' }
)
</script>

<style>
/* oCIS Web fits its content area to the window and hides what overflows it,
   so the page has to scroll itself. */
.groupware-page {
  min-height: 0;
  overflow-y: auto;
}
</style>
