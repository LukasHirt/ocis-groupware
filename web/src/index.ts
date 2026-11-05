import { computed } from 'vue'
import { useGettext } from 'vue3-gettext'
import { defineWebApplication } from '@ownclouders/web-pkg'
import type { AppMenuItemExtension, AppNavigationItem } from '@ownclouders/web-pkg'
import type { RouteRecordRaw } from 'vue-router'
import CalendarPage from './pages/CalendarPage.vue'
import ContactsPage from './pages/ContactsPage.vue'

// The application id. oCIS Web mounts every route below /<id> and prefixes
// route names with it. This module must have no export but the default one:
// the runtime takes the AMD module's value as the application script.
const appId = 'groupware'

export default defineWebApplication({
  setup() {
    const { $gettext } = useGettext()

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
      }
    ]

    const extensions = computed<AppMenuItemExtension[]>(() => [
      {
        id: `${appId}.app-menu-item`,
        type: 'appMenuItem',
        label: () => $gettext('Calendar and Contacts'),
        icon: 'calendar',
        path: `/${appId}`
      }
    ])

    return {
      appInfo: {
        id: appId,
        name: $gettext('Calendar and Contacts'),
        icon: 'calendar'
      },
      routes,
      navItems,
      extensions
    }
  }
})
