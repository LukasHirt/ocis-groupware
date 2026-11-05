/* eslint-disable vue/one-component-per-file -- several small test components per file */
import { describe, expect, it } from 'vitest'
import { defineComponent, h, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { SideBar, NoContentMessage, AppLoadingSpinner, useSideBar } from './mocks/webPkg'

// The stubs are registered globally by tests/unit/setup.ts, so a template can
// use the <oc-*> tags. These tests pin down what the application relies on.

function mountTemplate(template: string, setup: () => Record<string, unknown> = () => ({})) {
  return mount(defineComponent({ setup, template }), { attachTo: document.body })
}

describe('OcButton', () => {
  it('is a button of type button that passes clicks and attributes through', async () => {
    let clicks = 0
    const wrapper = mountTemplate(
      '<oc-button data-testid="b" @click="onClick">Go</oc-button>',
      () => ({ onClick: () => clicks++ })
    )
    const button = wrapper.get('[data-testid="b"]')
    expect(button.element.tagName).toBe('BUTTON')
    expect(button.attributes('type')).toBe('button')
    await button.trigger('click')
    expect(clicks).toBe(1)
  })

  it('submits a form when asked to, and is disabled when told so', () => {
    const wrapper = mountTemplate(
      '<form><oc-button submit="submit" :disabled="true" aria-label="Save">S</oc-button></form>'
    )
    const button = wrapper.get('button')
    expect(button.attributes('type')).toBe('submit')
    expect(button.attributes('disabled')).toBeDefined()
    expect(button.attributes('aria-label')).toBe('Save')
  })
})

describe('OcIcon and OcSpinner', () => {
  it('shows the icon name and gives the spinner a status role', () => {
    const wrapper = mountTemplate('<div><oc-icon name="add" /><oc-spinner aria-label="Loading" /></div>')
    expect(wrapper.get('.oc-icon').attributes('data-name')).toBe('add')
    expect(wrapper.get('[role="status"]').attributes('aria-label')).toBe('Loading')
  })
})

describe('OcTextInput', () => {
  it('forwards the test id to the input and labels it', () => {
    const wrapper = mountTemplate(
      '<oc-text-input id="gw-name" label="Name" model-value="Ada" data-testid="name" />'
    )
    const input = wrapper.get('input')
    expect(input.attributes('data-testid')).toBe('name')
    expect(input.element.value).toBe('Ada')
    expect(wrapper.get('label').attributes('for')).toBe('gw-name')
    expect(wrapper.get('label').text()).toBe('Name')
  })

  it('shows the error message in #<id>-message and wires it to the input', () => {
    const wrapper = mountTemplate(
      '<oc-text-input id="gw-name" label="Name" error-message="Enter a name" />'
    )
    expect(wrapper.get('#gw-name-message').text()).toBe('Enter a name')
    expect(wrapper.get('input').attributes('aria-describedby')).toBe('gw-name-message')
    expect(wrapper.get('input').attributes('aria-invalid')).toBe('true')
  })

  it('falls back to the description message and makes an id when none is given', () => {
    const wrapper = mountTemplate('<oc-text-input label="Search" description-message="Three letters" />')
    const id = wrapper.get('input').attributes('id')!
    expect(id).not.toBe('')
    expect(wrapper.get(`#${id}-message`).text()).toBe('Three letters')
  })

  it('emits update:modelValue on input and keeps the type', async () => {
    const value = ref('')
    const wrapper = mountTemplate(
      '<oc-text-input label="Day" type="date" v-model="value" data-testid="day" />',
      () => ({ value })
    )
    expect(wrapper.get('input').attributes('type')).toBe('date')
    await wrapper.get('input').setValue('2026-11-05')
    expect(value.value).toBe('2026-11-05')
  })
})

describe('OcTextarea', () => {
  it('is a textarea that takes the attributes and emits its value', async () => {
    const value = ref('')
    const wrapper = mountTemplate('<oc-textarea label="Notes" v-model="value" data-testid="notes" />', () => ({
      value
    }))
    expect(wrapper.get('textarea').attributes('data-testid')).toBe('notes')
    await wrapper.get('textarea').setValue('Bring slides')
    expect(value.value).toBe('Bring slides')
  })
})

describe('OcCheckbox', () => {
  it('puts the attributes on the root span and emits on change', async () => {
    const checked = ref(false)
    const wrapper = mountTemplate(
      '<oc-checkbox label="Show Work" v-model="checked" data-testid="toggle" />',
      () => ({ checked })
    )
    const root = wrapper.get('[data-testid="toggle"]')
    expect(root.element.tagName).toBe('SPAN')
    const input = root.get('input[type="checkbox"]')
    expect(input.attributes('aria-label')).toBe('Show Work')
    await input.setValue(true)
    expect(checked.value).toBe(true)
  })
})

describe('OcSelect', () => {
  const options = [
    { label: 'Weekly', value: 'weekly' },
    { label: 'Daily', value: 'daily' }
  ]

  it('emits the option itself without reduce', async () => {
    const chosen = ref<unknown>(null)
    const wrapper = mountTemplate('<oc-select label="Repeat" :options="options" v-model="chosen" />', () => ({
      options,
      chosen
    }))
    await wrapper.get('select').setValue('1')
    expect(chosen.value).toEqual(options[1])
  })

  it('emits the reduced value with reduce, and selects the option of the model', async () => {
    const chosen = ref('weekly')
    const wrapper = mountTemplate(
      '<oc-select label="Repeat" :options="options" :reduce="(o) => o.value" v-model="chosen" data-testid="freq" />',
      () => ({ options, chosen })
    )
    const select = wrapper.get('[data-testid="freq"]')
    expect((select.element as HTMLSelectElement).selectedIndex).toBe(0)
    await select.setValue('1')
    expect(chosen.value).toBe('daily')
    expect((select.element as HTMLSelectElement).selectedIndex).toBe(1)
  })

  it('takes plain strings as options', async () => {
    const chosen = ref('a')
    const wrapper = mountTemplate('<oc-select label="L" :options="[\'a\', \'b\']" v-model="chosen" />', () => ({
      chosen
    }))
    await wrapper.get('select').setValue('1')
    expect(chosen.value).toBe('b')
  })
})

describe('OcModal', () => {
  it('is a labelled dialog with its content', () => {
    const wrapper = mount(
      defineComponent({
        template: '<oc-modal title="Delete calendar"><template #content><p>Sure?</p></template></oc-modal>'
      })
    )
    const dialog = wrapper.get('[role="dialog"]')
    expect(dialog.attributes('aria-modal')).toBe('true')
    expect(wrapper.get(`#${dialog.attributes('aria-labelledby')}`).text()).toBe('Delete calendar')
    expect(dialog.text()).toContain('Sure?')
  })

  it('emits cancel on Escape and not on other keys', async () => {
    let cancels = 0
    const wrapper = mountTemplate('<oc-modal title="T" @cancel="onCancel"><template #content>x</template></oc-modal>', () => ({
      onCancel: () => cancels++
    }))
    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'a' })
    expect(cancels).toBe(0)
    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Escape' })
    expect(cancels).toBe(1)
  })
})

describe('OcTable', () => {
  it('has a row per item, field slots and a highlight event on click', async () => {
    const highlighted: unknown[] = []
    const data = [{ id: 'a', name: 'Ada' }, { id: 'b', name: 'Grace' }]
    const fields = [{ name: 'name', title: 'Name' }, { name: 'actions', title: '' }]
    const wrapper = mountTemplate(
      `<oc-table :data="data" :fields="fields" @highlight="(i) => seen.push(i)">
         <template #actions="{ item }"><button :data-id="item.id">edit</button></template>
       </oc-table>`,
      () => ({ data, fields, seen: highlighted })
    )
    const rows = wrapper.findAll('tbody tr')
    expect(rows.map((r) => r.text())).toEqual(['Adaedit', 'Graceedit'])
    expect(wrapper.get('th').text()).toBe('Name')
    await rows[1].trigger('click')
    expect(highlighted).toEqual([data[1]])
  })
})

describe('the other stubs', () => {
  it('render native elements, and the drop is always open', () => {
    const wrapper = mountTemplate(`
      <div>
        <oc-table-simple><oc-thead><oc-tr><oc-th>H</oc-th></oc-tr></oc-thead>
          <oc-tbody><oc-tr><oc-td>C</oc-td></oc-tr></oc-tbody></oc-table-simple>
        <oc-drop><button data-testid="inside">in</button></oc-drop>
        <oc-avatar>A</oc-avatar><oc-tag>T</oc-tag>
        <portal-target name="app.runtime.mobile.nav" />
      </div>`)
    expect(wrapper.get('table th').text()).toBe('H')
    expect(wrapper.get('table td').text()).toBe('C')
    expect(wrapper.find('[data-testid="inside"]').exists()).toBe(true)
    expect(wrapper.html()).not.toContain('portal-target')
  })
})

describe('the web-pkg mocks', () => {
  it('renders the active panel of the side bar with its attributes, when it is open', () => {
    const Details = defineComponent({
      props: { item: { type: String, default: '' } },
      setup: (p) => () => h('p', { 'data-testid': 'details' }, p.item)
    })
    const panels = [
      { name: 'details', isVisible: () => true, component: Details, componentAttrs: () => ({ item: 'Team sync' }) }
    ]
    const closed = mount(SideBar, { props: { isOpen: false, availablePanels: panels } })
    expect(closed.find('[data-testid="details"]').exists()).toBe(false)
    const open = mount(SideBar, { props: { isOpen: true, availablePanels: panels } })
    expect(open.get('[data-testid="details"]').text()).toBe('Team sync')
    return open.get('[data-testid="sidebar-close"]').trigger('click').then(() => {
      expect(open.emitted('close')).toHaveLength(1)
    })
  })

  it('renders the slots of the empty-state and a loading status', () => {
    const empty = mount(NoContentMessage, {
      props: { icon: 'calendar' },
      slots: { message: 'Nothing here', callToAction: '<button>Add</button>' }
    })
    expect(empty.text()).toContain('Nothing here')
    expect(empty.get('button').text()).toBe('Add')
    expect(mount(AppLoadingSpinner).find('[role="status"]').exists()).toBe(true)
  })

  it('shares one side bar state', () => {
    expect(useSideBar().isSideBarOpen.value).toBe(false)
  })
})
