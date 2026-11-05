/* eslint-disable vue/one-component-per-file -- several small test components per file */
import { defineComponent, h, type PropType, type VNode } from 'vue'

// Stand-ins for the design-system components that oCIS Web registers
// globally. The real ones are not part of this repository, so the unit tests
// register these instead. Each keeps the contract the application relies on:
// where attributes land, which events are emitted and which ids exist.

let counter = 0
const nextId = (prefix: string) => `${prefix}-${++counter}`

type Option = string | number | Record<string, unknown>

export const OcButton = defineComponent({
  name: 'OcButton',
  inheritAttrs: false,
  props: {
    submit: { type: String, default: 'button' },
    disabled: Boolean,
    ariaLabel: { type: String, default: undefined },
    variation: { type: String, default: undefined },
    appearance: { type: String, default: undefined },
    size: { type: String, default: undefined }
  },
  setup(props, { attrs, slots }) {
    return () =>
      h(
        'button',
        { ...attrs, type: props.submit, disabled: props.disabled, 'aria-label': props.ariaLabel },
        slots.default?.()
      )
  }
})

export const OcIcon = defineComponent({
  name: 'OcIcon',
  props: { name: { type: String, default: '' } },
  setup(props) {
    return () => h('span', { class: 'oc-icon', 'data-name': props.name })
  }
})

export const OcSpinner = defineComponent({
  name: 'OcSpinner',
  props: { ariaLabel: { type: String, default: undefined } },
  setup(props) {
    return () => h('span', { role: 'status', 'aria-label': props.ariaLabel })
  }
})

// A text input or a textarea. Attributes, data-testid among them, land on the
// field, as they do in the real component. The message is wired to the field
// through aria-describedby.
function fieldStub(name: string, tag: 'input' | 'textarea') {
  return defineComponent({
    name,
    inheritAttrs: false,
    props: {
      id: { type: String, default: undefined },
      label: { type: String, default: '' },
      type: { type: String, default: 'text' },
      modelValue: { type: [String, Number], default: '' },
      errorMessage: { type: String, default: '' },
      descriptionMessage: { type: String, default: '' },
      warningMessage: { type: String, default: '' }
    },
    emits: ['update:modelValue'],
    setup(props, { attrs, emit }) {
      const id = props.id ?? nextId('oc-field')
      return () => {
        const message = props.errorMessage || props.warningMessage || props.descriptionMessage
        const field = h(tag, {
          ...attrs,
          id,
          ...(tag === 'input' ? { type: props.type } : {}),
          value: props.modelValue,
          'aria-invalid': props.errorMessage ? 'true' : undefined,
          'aria-describedby': message ? `${id}-message` : undefined,
          onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLInputElement).value)
        })
        return h('div', [
          h('label', { for: id }, props.label),
          field,
          h('span', { id: `${id}-message` }, message)
        ])
      }
    }
  })
}
export const OcTextInput = fieldStub('OcTextInput', 'input')
export const OcTextarea = fieldStub('OcTextarea', 'textarea')

// The real checkbox puts the attributes on its root span and keeps the input
// inside, so a test id has to be followed by a lookup of the input.
export const OcCheckbox = defineComponent({
  name: 'OcCheckbox',
  inheritAttrs: false,
  props: {
    label: { type: String, default: '' },
    modelValue: { type: [Boolean, Array] as PropType<boolean | unknown[]>, default: false }
  },
  emits: ['update:modelValue'],
  setup(props, { attrs, emit }) {
    return () =>
      h('span', attrs, [
        h('input', {
          type: 'checkbox',
          checked: props.modelValue === true,
          'aria-label': props.label,
          onChange: (e: Event) => emit('update:modelValue', (e.target as HTMLInputElement).checked)
        })
      ])
  }
})

// A select. It emits the option, or what `reduce` makes of it, like vue-select.
export const OcSelect = defineComponent({
  name: 'OcSelect',
  inheritAttrs: false,
  props: {
    label: { type: String, default: '' },
    options: { type: Array as PropType<Option[]>, default: () => [] },
    optionLabel: { type: String, default: 'label' },
    reduce: { type: Function as PropType<(o: Option) => unknown>, default: undefined },
    modelValue: { type: null as unknown as PropType<unknown>, default: undefined },
    errorMessage: { type: String, default: '' },
    descriptionMessage: { type: String, default: '' }
  },
  emits: ['update:modelValue'],
  setup(props, { attrs, emit }) {
    const valueOf = (o: Option) => (props.reduce ? props.reduce(o) : o)
    const labelOf = (o: Option) =>
      typeof o === 'object' ? String(o[props.optionLabel] ?? '') : String(o)
    const sameValue = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b)
    return () =>
      h('div', [
        h('span', props.label),
        h(
          'select',
          {
            ...attrs,
            value: String(
              Math.max(
                0,
                props.options.findIndex((o) => sameValue(valueOf(o), props.modelValue))
              )
            ),
            onChange: (e: Event) => {
              const index = (e.target as HTMLSelectElement).selectedIndex
              emit('update:modelValue', valueOf(props.options[index]))
            }
          },
          props.options.map((o, i) =>
            h(
              'option',
              { key: i, value: String(i) },
              labelOf(o)
            )
          )
        ),
        h('span', props.errorMessage || props.descriptionMessage)
      ])
  }
})

// A dialog. Escape asks to cancel, as the real one does; the focus trap is
// not reproduced. The title carries the icon, which the real one draws from
// the icon prop.
export const OcModal = defineComponent({
  name: 'OcModal',
  props: { title: { type: String, default: '' }, icon: { type: String, default: '' } },
  emits: ['cancel', 'confirm'],
  setup(props, { emit, slots }) {
    const titleId = nextId('oc-modal-title')
    return () =>
      h(
        'div',
        {
          role: 'dialog',
          'aria-modal': 'true',
          'aria-labelledby': titleId,
          onKeydown: (e: KeyboardEvent) => {
            if (e.key === 'Escape') emit('cancel')
          }
        },
        [
          h('div', { class: 'oc-modal-title' }, [
            props.icon ? h(OcIcon, { name: props.icon }) : null,
            h('h2', { id: titleId }, props.title)
          ]),
          slots.content?.()
        ]
      )
  }
})

// A table with a row per item and the field slots, which is what the pages
// read. A click on a row highlights it.
export const OcTable = defineComponent({
  name: 'OcTable',
  props: {
    data: { type: Array as PropType<Record<string, unknown>[]>, default: () => [] },
    fields: { type: Array as PropType<{ name: string; title?: string }[]>, default: () => [] }
  },
  emits: ['highlight'],
  setup(props, { emit, slots }) {
    return () =>
      h('table', [
        h('thead', [h('tr', props.fields.map((f) => h('th', f.title ?? f.name)))]),
        h(
          'tbody',
          props.data.map((item, i) =>
            h(
              'tr',
              { key: i, onClick: () => emit('highlight', item) },
              props.fields.map((f) => {
                const slot = slots[`cell-${f.name}`] ?? slots[f.name]
                const content = slot ? slot({ item }) : String(item[f.name] ?? '')
                return h('td', content as VNode[] | string)
              })
            )
          )
        )
      ])
  }
})

function plain(name: string, tag: string) {
  return defineComponent({
    name,
    setup(_, { attrs, slots }) {
      return () => h(tag, attrs, slots.default?.())
    }
  })
}
export const OcTableSimple = plain('OcTableSimple', 'table')
export const OcThead = plain('OcThead', 'thead')
export const OcTbody = plain('OcTbody', 'tbody')
export const OcTr = plain('OcTr', 'tr')
export const OcTd = plain('OcTd', 'td')
export const OcTh = plain('OcTh', 'th')
export const OcAvatar = plain('OcAvatar', 'span')
export const OcTag = plain('OcTag', 'span')

// The drop is always rendered, so the items inside it can be found.
export const OcDrop = plain('OcDrop', 'div')

export const OcProgress = plain('OcProgress', 'div')

export const PortalTarget = defineComponent({
  name: 'PortalTarget',
  setup() {
    return () => null
  }
})

export const designSystemStubs = {
  OcButton,
  OcIcon,
  OcSpinner,
  OcTextInput,
  OcTextarea,
  OcCheckbox,
  OcSelect,
  OcModal,
  OcTable,
  OcTableSimple,
  OcThead,
  OcTbody,
  OcTr,
  OcTd,
  OcTh,
  OcDrop,
  OcAvatar,
  OcTag,
  OcProgress,
  PortalTarget
}
