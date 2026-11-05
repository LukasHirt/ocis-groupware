import type { ContactInput, GraphContact, PhysicalAddress } from '../api/types'

// A draft is what the contact form edits: plain strings, and lists of
// strings with room for a row that is still empty.
export interface Draft {
  displayName: string
  title: string
  givenName: string
  middleName: string
  surname: string
  generation: string
  nickName: string
  yomiGivenName: string
  yomiSurname: string
  yomiCompany: string
  companyName: string
  jobTitle: string
  department: string
  profession: string
  assistantName: string
  manager: string
  spouseName: string
  mobilePhone: string
  businessHomePage: string
  personalNotes: string
  // A date as YYYY-MM-DD, or empty.
  birthday: string
  emails: string[]
  businessPhones: string[]
  homePhones: string[]
  imAddresses: string[]
  children: string[]
  homeAddress: PhysicalAddress
  businessAddress: PhysicalAddress
  otherAddress: PhysicalAddress
}

export const textFields = [
  'displayName',
  'title',
  'givenName',
  'middleName',
  'surname',
  'generation',
  'nickName',
  'yomiGivenName',
  'yomiSurname',
  'yomiCompany',
  'companyName',
  'jobTitle',
  'department',
  'profession',
  'assistantName',
  'manager',
  'spouseName',
  'mobilePhone',
  'businessHomePage',
  'personalNotes'
] as const

export const listFields = ['businessPhones', 'homePhones', 'imAddresses', 'children'] as const

export const addressFields = ['homeAddress', 'businessAddress', 'otherAddress'] as const

const noAddress = (): PhysicalAddress => ({
  street: '',
  city: '',
  state: '',
  countryOrRegion: '',
  postalCode: ''
})

export function emptyDraft(): Draft {
  return {
    displayName: '',
    title: '',
    givenName: '',
    middleName: '',
    surname: '',
    generation: '',
    nickName: '',
    yomiGivenName: '',
    yomiSurname: '',
    yomiCompany: '',
    companyName: '',
    jobTitle: '',
    department: '',
    profession: '',
    assistantName: '',
    manager: '',
    spouseName: '',
    mobilePhone: '',
    businessHomePage: '',
    personalNotes: '',
    birthday: '',
    emails: [''],
    businessPhones: [''],
    homePhones: [''],
    imAddresses: [''],
    children: [''],
    homeAddress: noAddress(),
    businessAddress: noAddress(),
    otherAddress: noAddress()
  }
}

const rows = (list: string[]) => (list.length > 0 ? [...list] : [''])

// toDraft copies a contact into a draft. A birthday without a year (the
// server sends year 1) cannot be shown in a date field, so it stays empty
// and is left alone unless the user picks a date.
export function toDraft(c: GraphContact): Draft {
  const d = emptyDraft()
  for (const f of textFields) d[f] = c[f]
  d.birthday = c.birthday && !c.birthday.startsWith('0001') ? c.birthday.slice(0, 10) : ''
  d.emails = rows(c.emailAddresses.map((e) => e.address))
  for (const f of listFields) d[f] = rows(c[f])
  for (const f of addressFields) d[f] = { ...c[f] }
  return d
}

const clean = (list: string[]) => list.map((s) => s.trim()).filter(Boolean)

function cleanAddress(a: PhysicalAddress): PhysicalAddress {
  return {
    street: a.street.trim(),
    city: a.city.trim(),
    state: a.state.trim(),
    countryOrRegion: a.countryOrRegion.trim(),
    postalCode: a.postalCode.trim()
  }
}

// buildInput gives the fields of the contact that differ between the draft
// the form started from and the draft as edited. Blank rows are dropped and
// text is trimmed. A field that was emptied is sent empty, which clears it.
export function buildInput(before: Draft, after: Draft): ContactInput {
  const out: ContactInput = {}
  for (const f of textFields) {
    if (before[f].trim() !== after[f].trim()) out[f] = after[f].trim()
  }
  for (const f of listFields) {
    const next = clean(after[f])
    if (JSON.stringify(clean(before[f])) !== JSON.stringify(next)) out[f] = next
  }
  const emails = clean(after.emails)
  if (JSON.stringify(clean(before.emails)) !== JSON.stringify(emails)) {
    out.emailAddresses = emails.map((a) => ({ name: a, address: a }))
  }
  for (const f of addressFields) {
    const next = cleanAddress(after[f])
    if (JSON.stringify(cleanAddress(before[f])) !== JSON.stringify(next)) out[f] = next
  }
  if (before.birthday !== after.birthday) {
    out.birthday = after.birthday ? `${after.birthday}T00:00:00Z` : null
  }
  return out
}
