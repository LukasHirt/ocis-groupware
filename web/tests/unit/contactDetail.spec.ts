import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import contact from '../../../internal/graphapi/testdata/contract/contact.json'
import type { GraphContact } from '../../src/api/types'

vi.mock('vue3-gettext', () => ({
  useGettext: () => ({ $gettext: (s: string, p: Record<string, string> = {}) => s.replace(/%\{(\w+)\}/g, (_, k: string) => p[k] ?? '') })
}))
vi.mock('../../src/composables/useApi', () => ({
  useApi: () => ({ client: {}, run: () => Promise.resolve(undefined) })
}))

import ContactDetail from '../../src/contacts/ContactDetail.vue'

const marie = contact as unknown as GraphContact
const gettext = (s: string, p: Record<string, string> = {}) =>
  s.replace(/%\{(\w+)\}/g, (_, k: string) => p[k] ?? '')

async function show(over: Partial<GraphContact> = {}, canEdit = true) {
  const w = mount(ContactDetail, {
    props: { contact: { ...marie, ...over }, canEdit, suggestions: [] },
    global: { mocks: { $gettext: gettext } }
  })
  await flushPromises()
  return w
}

describe('the details of a contact', () => {
  it('shows the name and the job and company under it', async () => {
    const w = await show()
    expect(w.get('h3').text()).toBe('Dr. Marie Salomea Curie')
    expect(w.get('[data-testid="contact-subtitle"]').text()).toBe('Professor · Sorbonne')
  })

  it('says so for a contact without a name', async () => {
    const w = await show({ displayName: '', givenName: '', surname: '', emailAddresses: [], companyName: '' })
    expect(w.get('h3').text()).toBe('Unnamed contact')
  })

  it('makes emails and phone numbers links', async () => {
    const w = await show()
    const hrefs = w.findAll('a').map((a) => a.attributes('href'))
    expect(hrefs).toContain('mailto:marie@example.org')
    expect(hrefs).toContain('tel:+33155550101')
    expect(hrefs).toContain('tel:+33155550202')
    expect(hrefs).toContain('https://curie.example')
  })

  it('opens only web addresses as links', async () => {
    const w = await show({ businessHomePage: 'javascript:alert(1)' })
    expect(w.findAll('a').map((a) => a.attributes('href'))).not.toContain('javascript:alert(1)')
    const bare = await show({ businessHomePage: 'curie.example' })
    expect(bare.findAll('a').map((a) => a.attributes('href'))).toContain('https://curie.example')
  })

  it('lists only the fields that have a value, with a label each', async () => {
    const w = await show({ nickName: '', spouseName: '', children: [], businessPhones: [] })
    const labels = w.findAll('dt').map((d) => d.text())
    expect(labels).toEqual(
      expect.arrayContaining(['Email', 'Mobile', 'Home phone', 'Home address', 'Work address', 'Notes'])
    )
    expect(labels).not.toContain('Nickname')
    expect(labels).not.toContain('Spouse')
    expect(labels).not.toContain('Work phone')
    expect(labels).not.toContain('Other address')
  })

  it('shows an address on one line and keeps the lines of the notes', async () => {
    const w = await show()
    expect(w.text()).toContain('1 Rue du Four, 92330 Sceaux, IDF, France')
    expect(w.findAll('.gw-contact-line').filter((l) => /Radium|Two Nobel/.test(l.text()))).toHaveLength(2)
  })

  it('writes a birthday in the language of the browser, without a year when there is none', async () => {
    const format = (year: number | undefined, month: number, day: number) =>
      new Intl.DateTimeFormat(undefined, {
        day: 'numeric',
        month: 'long',
        ...(year ? { year: 'numeric' } : {}),
        timeZone: 'UTC'
      }).format(new Date(Date.UTC(year ?? 2000, month - 1, day)))
    const w = await show()
    expect(w.text()).toContain(format(undefined, 11, 7))
    expect(w.text()).not.toContain('0001')
    const dated = await show({ birthday: '1879-03-14T00:00:00Z' })
    expect(dated.text()).toContain(format(1879, 3, 14))
  })

  it('shows the groups of the contact and the group cards it belongs to', async () => {
    const w = await show()
    expect(w.findAll('[data-testid="membership-name"]').map((n) => n.text())).toEqual([
      'Physicists',
      'Nobel, laureates'
    ])
    expect(w.get('[data-testid="detail-membership"]').text()).toBe('Physicists')
  })

  it('has edit and delete for someone who may change the contact, and tells the page', async () => {
    const w = await show()
    await w.get('[data-testid="contact-edit"]').trigger('click')
    await w.get('[data-testid="contact-delete"]').trigger('click')
    expect(w.emitted('edit')).toHaveLength(1)
    expect(w.emitted('delete')).toHaveLength(1)
  })

  it('has no actions, no photo control and no group editing on a read-only contact', async () => {
    const w = await show({}, false)
    expect(w.find('[data-testid="contact-actions"]').exists()).toBe(false)
    expect(w.find('[data-testid="photo-input"]').exists()).toBe(false)
    expect(w.find('[data-testid="membership-remove"]').exists()).toBe(false)
    expect(w.find('[data-testid="membership-start"]').exists()).toBe(false)
  })
})
