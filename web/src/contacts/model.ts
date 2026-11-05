import type { GraphContact } from '../api/types'

export type SortKey = 'name' | 'surname' | 'company'

// contactName is what a list row shows for a contact: the display name, else
// the parts of the name, else the first email address, else the company. It
// is empty for a contact with none of them; the caller supplies a label.
export function contactName(c: GraphContact): string {
  if (c.displayName) return c.displayName
  const parts = [c.givenName, c.surname].filter(Boolean).join(' ')
  if (parts) return parts
  return c.emailAddresses[0]?.address || c.companyName || ''
}

function haystack(c: GraphContact): string {
  return [
    contactName(c),
    c.nickName,
    c.companyName,
    c.jobTitle,
    c.department,
    ...c.emailAddresses.flatMap((e) => [e.name, e.address]),
    ...c.businessPhones,
    ...c.homePhones,
    c.mobilePhone,
    ...c.categories
  ]
    .join('\n')
    .toLowerCase()
}

// filterContacts keeps the contacts that contain every word of the query in
// one of their searchable fields.
export function filterContacts(list: GraphContact[], query: string): GraphContact[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean)
  if (words.length === 0) return list
  return list.filter((c) => {
    const text = haystack(c)
    return words.every((w) => text.includes(w))
  })
}

const collator = new Intl.Collator(undefined, { sensitivity: 'base', numeric: true })

function sortValue(c: GraphContact, key: SortKey): string {
  switch (key) {
    case 'surname':
      return c.surname || contactName(c)
    case 'company':
      return c.companyName
    default:
      return contactName(c)
  }
}

// sortContacts returns a sorted copy. Accents and case do not matter, and a
// contact without a value for the key goes last.
export function sortContacts(list: GraphContact[], key: SortKey): GraphContact[] {
  return [...list].sort((a, b) => {
    const x = sortValue(a, key)
    const y = sortValue(b, key)
    if (!x !== !y) return x ? -1 : 1
    return collator.compare(x, y) || collator.compare(contactName(a), contactName(b))
  })
}
