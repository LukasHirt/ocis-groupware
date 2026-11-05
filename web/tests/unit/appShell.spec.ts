import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import AppShell from '../../src/components/AppShell.vue'
import { eventBus, isSideBarOpen, sideBarActivePanel } from './mocks/webPkg'

vi.mock('@ownclouders/web-pkg', async () => (await import('./mocks/webPkg')).webPkgMock())

// The window is wide unless a test says otherwise.
function setWidth(width: number) {
  window.matchMedia = ((query: string) => {
    const max = Number(/max-width: (\d+)px/.exec(query)?.[1] ?? 0)
    return {
      matches: width <= max,
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn()
    }
  }) as unknown as typeof window.matchMedia
}

const Details = defineComponent({
  props: { item: { type: String, default: '' } },
  setup: (props) => () => h('p', { 'data-testid': 'details' }, props.item)
})
const panels = [
  {
    name: 'details',
    icon: 'information',
    title: () => 'Details',
    isVisible: () => true,
    isRoot: () => true,
    component: Details,
    componentAttrs: () => ({ item: 'Team sync' })
  }
]

beforeEach(() => {
  setWidth(1800)
  isSideBarOpen.value = false
  sideBarActivePanel.value = undefined
  eventBus.publish.mockClear()
})

describe('AppShell', () => {
  it('shows the title as the one h1 and the page in its body', () => {
    const wrapper = mount(AppShell, { props: { title: 'Calendar' }, slots: { default: '<p id="page">Page</p>' } })
    expect(wrapper.findAll('h1')).toHaveLength(1)
    expect(wrapper.get('h1').text()).toBe('Calendar')
    expect(wrapper.get('.gw-body').text()).toContain('Page')
  })

  it('lets a page replace the title', () => {
    const wrapper = mount(AppShell, {
      props: { title: 'Calendar' },
      slots: { title: '<h1 data-testid="own">November 2026</h1>' }
    })
    expect(wrapper.findAll('h1')).toHaveLength(1)
    expect(wrapper.get('h1').text()).toBe('November 2026')
  })

  it('shows a spinner instead of the page while it loads, and keeps the title', () => {
    const wrapper = mount(AppShell, { props: { title: 'Contacts', loading: true }, slots: { default: '<p id="page">Page</p>' } })
    expect(wrapper.find('[data-testid="app-loading"]').exists()).toBe(true)
    expect(wrapper.find('#page').exists()).toBe(false)
    expect(wrapper.get('h1').text()).toBe('Contacts')
  })

  it('has a target for the mobile navigation of oCIS Web in the app bar', () => {
    // The stub target renders nothing, so the shell is checked by its markup.
    const wrapper = mount(AppShell, { props: { title: 'T' }, global: { stubs: { PortalTarget: { template: '<i data-testid="mobile-nav" />' } } } })
    expect(wrapper.find('.gw-app-bar [data-testid="mobile-nav"]').exists()).toBe(true)
  })

  it('shows the rows of controls only when a page gives them', () => {
    const bare = mount(AppShell, { props: { title: 'T' } })
    expect(bare.find('.gw-app-actions').exists()).toBe(false)
    const full = mount(AppShell, {
      props: { title: 'T' },
      slots: { actions: '<button data-testid="main">New</button>', viewControls: '<button data-testid="view">Week</button>' }
    })
    expect(full.find('.gw-app-actions [data-testid="main"]').exists()).toBe(true)
    expect(full.find('[data-testid="view"]').exists()).toBe(true)
  })

  describe('the list pane', () => {
    const slots = { list: '<ul data-testid="list"><li>Personal</li></ul>' }

    it('sits beside the body on a wide window', () => {
      const wrapper = mount(AppShell, { props: { title: 'T' }, slots })
      expect(wrapper.find('.gw-list-pane [data-testid="list"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="list-toggle"]').exists()).toBe(false)
    })

    it('folds under a button on a narrow window', () => {
      setWidth(900)
      const wrapper = mount(AppShell, { props: { title: 'T', listToggleLabel: 'Show calendars' }, slots })
      expect(wrapper.find('.gw-list-pane').exists()).toBe(false)
      const toggle = wrapper.get('[data-testid="list-toggle"]')
      expect(toggle.attributes('aria-label')).toBe('Show calendars')
      expect(wrapper.find('.gw-list-drop [data-testid="list"]').exists()).toBe(true)
    })

    it('folds on a medium window while the sidebar is open', async () => {
      setWidth(1400)
      const wrapper = mount(AppShell, { props: { title: 'T' }, slots })
      expect(wrapper.find('.gw-list-pane').exists()).toBe(true)
      isSideBarOpen.value = true
      await nextTick()
      expect(wrapper.find('.gw-list-pane').exists()).toBe(false)
      expect(wrapper.find('[data-testid="list-toggle"]').exists()).toBe(true)
    })

    it('is folded when the page says it is closed', () => {
      const wrapper = mount(AppShell, { props: { title: 'T', listOpen: false }, slots })
      expect(wrapper.find('.gw-list-pane').exists()).toBe(false)
    })

    it('does not exist without a list', () => {
      setWidth(900)
      const wrapper = mount(AppShell, { props: { title: 'T' } })
      expect(wrapper.find('.gw-list-pane').exists()).toBe(false)
      expect(wrapper.find('[data-testid="list-toggle"]').exists()).toBe(false)
    })
  })

  describe('the sidebar', () => {
    it('is not shown while it is closed', () => {
      const wrapper = mount(AppShell, { props: { title: 'T', sideBarPanels: panels } })
      expect(wrapper.find('[data-testid="sidebar"]').exists()).toBe(false)
    })

    it('shows the panel with its attributes while it is open', () => {
      isSideBarOpen.value = true
      const wrapper = mount(AppShell, { props: { title: 'T', sideBarPanels: panels } })
      expect(wrapper.get('[data-testid="details"]').text()).toBe('Team sync')
    })

    it('is not shown when there are no panels', () => {
      isSideBarOpen.value = true
      const wrapper = mount(AppShell, { props: { title: 'T' } })
      expect(wrapper.find('[data-testid="sidebar"]').exists()).toBe(false)
    })

    it('closes through the event bus of oCIS Web', async () => {
      isSideBarOpen.value = true
      const wrapper = mount(AppShell, { props: { title: 'T', sideBarPanels: panels } })
      await wrapper.get('[data-testid="sidebar-close"]').trigger('click')
      expect(eventBus.publish).toHaveBeenCalledWith('sidebar.close')
    })
  })
})
