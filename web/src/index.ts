import { computed } from 'vue'
import { useGettext } from 'vue3-gettext'
import { defineWebApplication } from '@ownclouders/web-pkg'
import type { AppMenuItemExtension, AppNavigationItem } from '@ownclouders/web-pkg'
import type { RouteRecordRaw } from 'vue-router'
import CalendarPage from './pages/CalendarPage.vue'
import ContactsPage from './pages/ContactsPage.vue'
import DevicesPage from './pages/DevicesPage.vue'
import { useLeaseRenewal } from './composables/useLeaseRenewal'
import { useArea } from './composables/useArea'
import icon from './assets/icon.svg?inline'
import translations from './l10n/translations.json'

// The application id. oCIS Web mounts every route below /<id>; the route
// names below carry the id as a prefix. The id must not be a path that the
// proxy sends to the service (/groupware/, /dav/, /graph/ and so on): a reload
// of an application page would then reach the service instead of oCIS Web.
// This module must have no export but the default one: the runtime takes the
// AMD module's value as the application script.
const appId = 'organizer'

// The colours of the two entries in the application menu.
const calendarColor = '#1f6f8b'
const contactsColor = '#2f9e6e'

export default defineWebApplication({
  setup() {
    const { $gettext } = useGettext()

    // Confirms the user with the service once per page load, whichever app
    // of oCIS Web is open. See useLeaseRenewal.
    useLeaseRenewal()

    // One application serves two areas. Each area has its own entry in the
    // application menu and its own navigation, and both reach the Devices page.
    const area = useArea(appId)

    const user = { authContext: 'user' as const }
    const routes: RouteRecordRaw[] = [
      { path: '/', redirect: { name: `${appId}-calendar-events` } },
      { path: '/calendar', redirect: { name: `${appId}-calendar-events` } },
      {
        path: '/calendar/events',
        name: `${appId}-calendar-events`,
        component: CalendarPage,
        meta: { ...user, title: $gettext('Calendar') }
      },
      {
        path: '/calendar/devices',
        name: `${appId}-calendar-devices`,
        component: DevicesPage,
        meta: { ...user, title: $gettext('Devices') }
      },
      { path: '/contacts', redirect: { name: `${appId}-contacts-list` } },
      {
        path: '/contacts/list',
        name: `${appId}-contacts-list`,
        component: ContactsPage,
        meta: { ...user, title: $gettext('Contacts') }
      },
      {
        path: '/contacts/devices',
        name: `${appId}-contacts-devices`,
        component: DevicesPage,
        meta: { ...user, title: $gettext('Devices') }
      }
    ]

    const navItems: AppNavigationItem[] = [
      {
        name: $gettext('Calendar'),
        icon: 'calendar',
        priority: 10,
        route: { name: `${appId}-calendar-events` },
        isVisible: () => area() === 'calendar'
      },
      {
        name: $gettext('Devices'),
        icon: 'smartphone',
        priority: 90,
        route: { name: `${appId}-calendar-devices` },
        isVisible: () => area() === 'calendar'
      },
      {
        name: $gettext('Contacts'),
        icon: 'contacts-book',
        priority: 10,
        route: { name: `${appId}-contacts-list` },
        isVisible: () => area() === 'contacts'
      },
      {
        name: $gettext('Devices'),
        icon: 'smartphone',
        priority: 90,
        route: { name: `${appId}-contacts-devices` },
        isVisible: () => area() === 'contacts'
      }
    ]

    // The application menu lists applications through extensions, so one
    // application can add two entries. The menu marks an entry as active when
    // the route path starts with its path.
    const extensions = computed<AppMenuItemExtension[]>(() => [
      {
        id: `${appId}.calendar.app-menu-item`,
        type: 'appMenuItem',
        label: () => $gettext('Calendar'),
        icon: 'calendar',
        color: calendarColor,
        path: `/${appId}/calendar`
      },
      {
        id: `${appId}.contacts.app-menu-item`,
        type: 'appMenuItem',
        label: () => $gettext('Contacts'),
        icon: 'contacts-book',
        color: contactsColor,
        path: `/${appId}/contacts`
      }
    ])

    return {
      appInfo: {
        id: appId,
        name: $gettext('Calendar and Contacts'),
        icon: 'calendar',
        // The application menu shows a named icon of the design system; the
        // image, a data URI so that no extra file is served, is for the
        // places that take one.
        img: icon
      },
      translations,
      routes,
      navItems,
      extensions
    }
  }
})
