import type { VueWrapper } from '@vue/test-utils'

// The stand-in for oc-select renders a native select whose options carry
// their index as value. These helpers work with the visible labels instead.

type Mounted = Pick<VueWrapper, 'get' | 'findAll'>

const selector = (testId: string) => `[data-testid="${testId}"]`

export function optionLabels(wrapper: Mounted, testId: string): string[] {
  return wrapper.get(selector(testId)).findAll('option').map((o) => o.text())
}

export function selectedLabel(wrapper: Mounted, testId: string): string {
  const select = wrapper.get(selector(testId)).element as HTMLSelectElement
  return select.selectedOptions[0]?.textContent ?? ''
}

export async function choose(wrapper: Mounted, testId: string, label: string) {
  const select = wrapper.get(selector(testId))
  const option = select.findAll('option').find((o) => o.text() === label)
  if (!option) {
    throw new Error(`no option "${label}" in ${testId}`)
  }
  await select.setValue((option.element as HTMLOptionElement).value)
}
