/* eslint-disable vue/one-component-per-file -- several small test components per file */
import { defineComponent, h, ref, type Component } from 'vue'
import { vi } from 'vitest'

// What the application takes from @ownclouders/web-pkg. The package pulls in
// the whole design system, so the specs replace it:
//
//   vi.mock('@ownclouders/web-pkg', async () => (await import('../mocks/webPkg')).webPkgMock())
//
// webPkgMock takes overrides for the parts a spec wants to control.

export const SideBarEventTopics = {
  open: 'sidebar.open',
  close: 'sidebar.close',
  toggle: 'sidebar.toggle',
  openWithPanel: 'sidebar.open-with-panel',
  setActivePanel: 'sidebar.set-active-panel'
} as const

export const eventBus = { publish: vi.fn(), subscribe: vi.fn(), unsubscribe: vi.fn() }

export const isSideBarOpen = ref(false)
export const sideBarActivePanel = ref<string | undefined>(undefined)
export const useSideBar = () => ({ isSideBarOpen, sideBarActivePanel })

interface Panel {
  name: string
  isVisible?: (ctx: unknown) => boolean
  component: Component
  componentAttrs?: (ctx: unknown) => Record<string, unknown>
}

// Renders the component of the active panel, or of the first visible one,
// with its attributes.
export const SideBar = defineComponent({
  name: 'SideBar',
  props: {
    isOpen: Boolean,
    availablePanels: { type: Array as () => Panel[], default: () => [] },
    panelContext: { type: Object, default: () => ({}) },
    activePanel: { type: String, default: undefined }
  },
  emits: ['close', 'selectPanel'],
  setup(props, { emit }) {
    return () => {
      if (!props.isOpen) return null
      const visible = props.availablePanels.filter((p) => p.isVisible?.(props.panelContext) ?? true)
      const panel = visible.find((p) => p.name === props.activePanel) ?? visible[0]
      return h('aside', { 'data-testid': 'sidebar' }, [
        h('button', { type: 'button', 'data-testid': 'sidebar-close', onClick: () => emit('close') }),
        panel ? h(panel.component, panel.componentAttrs?.(props.panelContext) ?? {}) : null
      ])
    }
  }
})

export const NoContentMessage = defineComponent({
  name: 'NoContentMessage',
  props: { icon: { type: String, default: '' } },
  setup(props, { slots }) {
    return () =>
      h('div', { 'data-icon': props.icon }, [slots.message?.(), slots.callToAction?.()])
  }
})

export const AppLoadingSpinner = defineComponent({
  name: 'AppLoadingSpinner',
  setup() {
    return () => h('div', { role: 'status', 'data-testid': 'app-loading' })
  }
})

export function webPkgMock(overrides: Record<string, unknown> = {}) {
  return {
    defineWebApplication: (args: unknown) => args,
    useAuthStore: () => ({ userContextReady: false, publicLinkContextReady: false }),
    useRouter: () => ({ currentRoute: ref({ name: undefined }) }),
    useMessages: () => ({ showErrorMessage: vi.fn(), showMessage: vi.fn() }),
    useClientService: () => ({ httpAuthenticated: { request: vi.fn() } }),
    useSideBar,
    eventBus,
    SideBarEventTopics,
    SideBar,
    NoContentMessage,
    AppLoadingSpinner,
    ...overrides
  }
}
