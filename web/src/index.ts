import { computed } from 'vue'
import { useGettext } from 'vue3-gettext'
import { defineWebApplication } from '@ownclouders/web-pkg'
import type { AppMenuItemExtension, AppNavigationItem } from '@ownclouders/web-pkg'
import type { RouteRecordRaw } from 'vue-router'
import CalendarPage from './pages/CalendarPage.vue'
import ContactsPage from './pages/ContactsPage.vue'
import AccountPage from './pages/AccountPage.vue'
import { useLeaseRenewal } from './composables/useLeaseRenewal'
import icon from './assets/icon.svg?inline'
import translations from './l10n/translations.json'

// The application id. oCIS Web mounts every route below /<id> and prefixes
// route names with it. This module must have no export but the default one:
// the runtime takes the AMD module's value as the application script.
const appId = 'groupware'

// The brand colour of the icon, also the colour of the application menu entry.
const appColor = '#1f6f8b'

export default defineWebApplication({
  setup() {
    const { $gettext } = useGettext()

    // Confirms the user with the service once per page load, whichever app
    // of oCIS Web is open. See useLeaseRenewal.
    useLeaseRenewal()

    const routes: RouteRecordRaw[] = [
      {
        path: '/',
        redirect: { name: `${appId}-calendar` }
      },
      {
        path: '/calendar',
        name: `${appId}-calendar`,
        component: CalendarPage,
        meta: { authContext: 'user', title: $gettext('Calendar') }
      },
      {
        path: '/contacts',
        name: `${appId}-contacts`,
        component: ContactsPage,
        meta: { authContext: 'user', title: $gettext('Contacts') }
      },
      {
        path: '/account',
        name: `${appId}-account`,
        component: AccountPage,
        meta: { authContext: 'user', title: $gettext('Account') }
      }
    ]

    const navItems: AppNavigationItem[] = [
      {
        name: $gettext('Calendar'),
        icon: 'calendar',
        route: { name: `${appId}-calendar` }
      },
      {
        name: $gettext('Contacts'),
        icon: 'contacts-book',
        route: { name: `${appId}-contacts` }
      },
      {
        name: $gettext('Account'),
        icon: 'user',
        route: { name: `${appId}-account` }
      }
    ]

    const extensions = computed<AppMenuItemExtension[]>(() => [
      {
        id: `${appId}.app-menu-item`,
        type: 'appMenuItem',
        label: () => $gettext('Calendar and Contacts'),
        icon: 'calendar',
        color: appColor,
        path: `/${appId}`
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
