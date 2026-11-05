<template>
  <main class="gw-app oc-flex oc-height-1-1 app-content oc-width-1-1">
    <aside v-if="$slots.list && listVisible" class="gw-list-pane" data-testid="list-pane">
      <slot name="list" :nested="false" />
    </aside>
    <div class="gw-main oc-width-expand oc-height-1-1">
      <div class="gw-app-bar oc-py-s oc-px-m">
        <div class="oc-flex oc-flex-between oc-flex-middle">
          <div class="oc-flex oc-flex-middle">
            <portal-target name="app.runtime.mobile.nav" />
            <template v-if="$slots.list && !listVisible">
              <oc-button
                id="gw-list-toggle"
                appearance="raw"
                class="oc-mr-s"
                :aria-label="listToggleLabel"
                data-testid="list-toggle"
              >
                <oc-icon name="menu-unfold" fill-type="line" />
              </oc-button>
              <oc-drop
                drop-id="gw-list-drop"
                toggle="#gw-list-toggle"
                mode="click"
                padding-size="small"
                class="gw-list-drop"
              >
                <!-- A drop inside this one has to say so. -->
                <slot name="list" :nested="true" />
              </oc-drop>
            </template>
            <slot name="title">
              <h1 class="gw-app-title">{{ title }}</h1>
            </slot>
          </div>
          <div class="oc-flex oc-flex-middle">
            <slot name="viewControls" />
          </div>
        </div>
        <div v-if="$slots.actions" class="gw-app-actions oc-flex oc-flex-middle oc-mt-xs">
          <slot name="actions" />
        </div>
      </div>
      <div class="gw-body">
        <app-loading-spinner v-if="loading" />
        <slot v-else />
      </div>
    </div>
    <side-bar
      v-if="isSideBarOpen && sideBarPanels.length > 0"
      class="gw-sidebar"
      :is-open="isSideBarOpen"
      :loading="false"
      :available-panels="sideBarPanels"
      :panel-context="sideBarContext"
      :active-panel="activePanel"
      @select-panel="(name: string) => (selectedPanel = name)"
      @close="closeSideBar"
    />
  </main>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  AppLoadingSpinner,
  SideBar,
  SideBarEventTopics,
  eventBus,
  useSideBar
} from '@ownclouders/web-pkg'
import type { SideBarPanel, SideBarPanelContext } from '@ownclouders/web-pkg'
import type { Item } from '@ownclouders/web-client'

defineOptions({ name: 'AppShell' })

// The frame of every page of the application: an app bar with the title and
// two rows for controls, an optional list pane on the left, the body, and the
// details sidebar of oCIS Web on the right. It follows the structure of the
// apps that oCIS Web ships, so the application looks like one of them.
const props = withDefaults(
  defineProps<{
    // The title of the view, the page's only h1.
    title: string
    loading?: boolean
    // Panels of the sidebar, shown while the sidebar is open. The sidebar is
    // opened and closed through the event bus of oCIS Web, so the toggle in
    // its top bar works too.
    sideBarPanels?: SideBarPanel<Item, Item, Item>[]
    sideBarContext?: SideBarPanelContext<Item, Item, Item>
    // Whether the list pane takes room beside the body. It also folds away
    // by itself on a narrow window, see below.
    listOpen?: boolean
    // The name of the button that opens the list when the pane is folded.
    listToggleLabel?: string
  }>(),
  { loading: false, sideBarPanels: () => [], sideBarContext: () => ({}), listOpen: true, listToggleLabel: '' }
)

const { isSideBarOpen, sideBarActivePanel } = useSideBar()
const selectedPanel = ref<string | undefined>(undefined)
const activePanel = computed(
  () => selectedPanel.value ?? (sideBarActivePanel.value || props.sideBarPanels[0]?.name)
)
watch(sideBarActivePanel, (name) => {
  if (name) selectedPanel.value = name
})

function closeSideBar() {
  eventBus.publish(SideBarEventTopics.close)
}

// The list folds into a drop under a button below 1200px, and while the
// sidebar is open below 1600px, so the body keeps room. The values are the
// design system's breakpoints.
const narrow = window.matchMedia('(max-width: 1199px)')
const medium = window.matchMedia('(max-width: 1599px)')
const folded = ref(false)
function measure() {
  folded.value = narrow.matches || (isSideBarOpen.value && medium.matches)
}
measure()
watch(isSideBarOpen, measure)
onMounted(() => {
  narrow.addEventListener('change', measure)
  medium.addEventListener('change', measure)
})
onBeforeUnmount(() => {
  narrow.removeEventListener('change', measure)
  medium.removeEventListener('change', measure)
})
const listVisible = computed(() => props.listOpen && !folded.value)
</script>

<style>
.gw-app {
  min-height: 0;
}
.gw-list-pane {
  flex: 0 0 15rem;
  border-right: 1px solid var(--oc-color-border);
  overflow-y: auto;
  padding: var(--oc-space-medium) var(--oc-space-small);
}
.gw-main {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
}
.gw-app-title {
  font-size: var(--oc-font-size-large);
  font-weight: var(--oc-font-weight-semibold);
  margin: 0;
}
/* The second row of the app bar wraps onto a further line on a narrow window,
   with the buttons keeping their labels on one line each. */
.gw-app-actions {
  flex-wrap: wrap;
  row-gap: var(--oc-space-xsmall);
}
.gw-app-actions .oc-button {
  white-space: nowrap;
}
.gw-body {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
  padding: 0 var(--oc-space-medium) var(--oc-space-medium);
}

/* The list, as the navigation of oCIS Web draws its items. */
.gw-list-item {
  display: flex;
  align-items: center;
  gap: var(--oc-space-small);
  height: 2.25rem;
  padding: 0 var(--oc-space-small);
  border-radius: 5px;
}
.gw-list-item:hover {
  background: var(--oc-color-background-hover);
}
.gw-list-item.selected {
  background: var(--oc-color-background-highlight);
  font-weight: var(--oc-font-weight-semibold);
  color: var(--oc-color-text-default);
}
.gw-list-heading {
  font-size: var(--oc-font-size-small);
  text-transform: none;
  color: var(--oc-color-text-muted);
  margin: var(--oc-space-medium) 0 var(--oc-space-xsmall);
}
.gw-count {
  margin-left: auto;
  color: var(--oc-color-text-muted);
  font-size: var(--oc-font-size-small);
}
</style>
