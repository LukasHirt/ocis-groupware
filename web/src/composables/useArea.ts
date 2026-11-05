import { useRouter } from '@ownclouders/web-pkg'

// The application serves two areas that oCIS Web shows as two entries of its
// application menu. Each area has its own navigation.
export type Area = 'calendar' | 'contacts'

export const areas: Area[] = ['calendar', 'contacts']

// areaOfRoute tells the area of a route by its name, which is the application
// id, the area and the page: "<id>-calendar-events". A route outside the
// application, or one that only redirects, has no area.
export function areaOfRoute(appId: string, name: unknown): Area | undefined {
  if (typeof name !== 'string') return undefined
  return areas.find((area) => name.startsWith(`${appId}-${area}-`))
}

// useArea returns a function that reads the area of the current route. oCIS
// Web evaluates the visibility of a navigation item whenever the route
// changes, so an item can show itself only in its own area.
export function useArea(appId: string): () => Area | undefined {
  const router = useRouter()
  return () => areaOfRoute(appId, router.currentRoute.value.name)
}
