import type { GraphContact } from '../api/types'

// A contact group is a category on the contacts that belong to it. Group
// cards, which some clients write, are listed too but never written.
export type GroupFilter = { kind: 'category'; name: string } | { kind: 'card'; id: string } | null

const collator = new Intl.Collator(undefined, { sensitivity: 'base', numeric: true })

export function listGroups(contacts: GraphContact[]): { name: string; count: number }[] {
  const counts = new Map<string, number>()
  for (const c of contacts) {
    for (const name of new Set(c.categories)) counts.set(name, (counts.get(name) ?? 0) + 1)
  }
  return [...counts]
    .map(([name, count]) => ({ name, count }))
    .sort((a, b) => collator.compare(a.name, b.name) || (a.name < b.name ? -1 : 1))
}

export function listGroupCards(
  contacts: GraphContact[]
): { id: string; name: string; count: number }[] {
  const cards = new Map<string, { id: string; name: string; count: number }>()
  for (const c of contacts) {
    for (const g of c['groupware.groups']) {
      const known = cards.get(g.id)
      if (known) known.count++
      else cards.set(g.id, { id: g.id, name: g.displayName, count: 1 })
    }
  }
  return [...cards.values()].sort((a, b) => collator.compare(a.name, b.name))
}

// addToGroup returns the categories with the group added. A blank name, or a
// group the contact has already under another spelling of case, changes nothing.
export function addToGroup(categories: string[], name: string): string[] {
  const n = name.trim()
  if (!n || categories.some((c) => c.toLowerCase() === n.toLowerCase())) return categories
  return [...categories, n]
}

export function removeFromGroup(categories: string[], name: string): string[] {
  return categories.filter((c) => c !== name)
}

export function renameGroup(categories: string[], from: string, to: string): string[] {
  const out: string[] = []
  for (const c of categories) {
    const name = c === from ? to : c
    if (!out.includes(name)) out.push(name)
  }
  return out
}

export function inGroup(c: GraphContact, filter: GroupFilter): boolean {
  if (!filter) return true
  if (filter.kind === 'category') return c.categories.includes(filter.name)
  return c['groupware.groups'].some((g) => g.id === filter.id)
}
