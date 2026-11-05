import type { Client } from '../api/client'
import type { Grantee } from '../api/types'

// The directory answers a search only from this many characters on.
export const minSearchLength = 3

export interface Sharee extends Grantee {
  name: string
}

// searchSharees looks for users and groups whose name matches the text, and
// leaves out those the collection is already shared with. Either search may
// fail on its own, for example when the directory hides groups from the
// user; the other still counts.
export async function searchSharees(
  client: Pick<Client, 'searchUsers' | 'searchGroups'>,
  term: string,
  taken: { type: string; id: string }[]
): Promise<Sharee[]> {
  const [users, groups] = await Promise.all([
    client.searchUsers(term).catch(() => []),
    client.searchGroups(term).catch(() => [])
  ])
  const all: Sharee[] = [
    ...users.map((u) => ({ type: 'user' as const, id: u.id, name: u.displayName })),
    ...groups.map((g) => ({ type: 'group' as const, id: g.id, name: g.displayName }))
  ]
  return all.filter((s) => !taken.some((t) => t.type === s.type && t.id === s.id))
}
