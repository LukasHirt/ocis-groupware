<template>
  <app-shell
    :title="view.title || $gettext('Calendar')"
    :loading="loading"
    :list-toggle-label="$gettext('Show calendars')"
    :side-bar-panels="panels"
  >
    <template #list="{ nested }">
      <calendar-list
        :calendars="calendars"
        :nested="nested"
        :hidden="hidden"
        @toggle="toggle"
        @add="dialog = { kind: 'create' }"
        @edit="(calendar: GraphCalendar) => (dialog = { kind: 'edit', calendar })"
        @share="(calendar: GraphCalendar) => (dialog = { kind: 'share', calendar })"
        @remove="(calendar: GraphCalendar) => (dialog = { kind: 'delete', calendar })"
        @import="chooseFile"
        @export="exportCalendar"
      />
    </template>
    <template v-if="calendars.length > 0" #title>
      <!-- On a phone the stepping buttons move to the second row, to leave
           room for the title. -->
      <calendar-steps v-if="!phone" :view="view" @today="grid?.today()" @prev="grid?.prev()" @next="grid?.next()" />
      <h1 class="gw-app-title" :class="{ 'oc-ml-s': !phone }" aria-live="polite" data-testid="calendar-title">
        {{ view.title }}
      </h1>
    </template>
    <template v-if="calendars.length > 0" #viewControls>
      <div v-if="!phone" class="oc-button-group oc-mr-s" role="group" :aria-label="$gettext('View')">
        <oc-button
          v-for="v in views"
          :key="v.type"
          variation="primary"
          :appearance="view.type === v.type ? 'filled' : 'outline'"
          size="small"
          :aria-pressed="view.type === v.type ? 'true' : 'false'"
          @click="grid?.changeView(v.type)"
        >
          {{ v.label }}
        </oc-button>
      </div>
      <template v-else>
        <oc-button id="gw-view-toggle" variation="passive" appearance="outline" size="small" class="oc-mr-s">
          {{ currentViewLabel }}
          <oc-icon name="arrow-down-s" fill-type="line" size="small" />
        </oc-button>
        <oc-drop drop-id="gw-view-drop" toggle="#gw-view-toggle" mode="click" close-on-click padding-size="small">
          <ul class="oc-list">
            <li v-for="v in views" :key="v.type">
              <oc-button
                appearance="raw"
                justify-content="left"
                class="oc-width-1-1"
                :aria-pressed="view.type === v.type ? 'true' : 'false'"
                @click="grid?.changeView(v.type)"
              >
                {{ v.label }}
              </oc-button>
            </li>
          </ul>
        </oc-drop>
      </template>
      <oc-button appearance="raw" :aria-label="$gettext('Calendar settings')" data-testid="settings-open" @click="dialog = { kind: 'settings' }">
        <oc-icon name="settings-3" fill-type="line" />
      </oc-button>
    </template>
    <template v-if="calendars.length > 0" #actions>
      <template v-if="phone">
        <calendar-steps :view="view" @today="grid?.today()" @prev="grid?.prev()" @next="grid?.next()" />
        <span class="gw-spacer" />
        <oc-button
          variation="primary"
          appearance="filled"
          size="small"
          :aria-label="$gettext('New event')"
          :disabled="writable.length === 0"
          data-testid="event-add"
          @click="newEvent()"
        >
          <oc-icon name="add" fill-type="line" size="small" />
          {{ $gettext('New event') }}
        </oc-button>
      </template>
      <template v-else>
      <oc-button
        v-oc-tooltip="writable.length === 0 ? $gettext('You have no calendar you can write to') : ''"
        variation="primary"
        appearance="filled"
        class="oc-mr-s"
        :disabled="writable.length === 0"
        data-testid="event-add"
        @click="newEvent()"
      >
        <oc-icon name="add" fill-type="line" size="small" />
        {{ $gettext('New event') }}
      </oc-button>
      <oc-button id="gw-import-toggle" appearance="raw" :disabled="writable.length === 0" data-testid="import-open">
        <oc-icon name="upload" fill-type="line" size="small" />
        {{ $gettext('Import') }}
      </oc-button>
      <oc-drop drop-id="gw-import-drop" toggle="#gw-import-toggle" mode="click" close-on-click padding-size="small">
        <ul class="oc-list">
          <li v-for="c in writable" :key="c.id">
            <oc-button
              appearance="raw"
              justify-content="left"
              class="oc-width-1-1"
              data-testid="import-into"
              @click="chooseFile(c)"
            >
              {{ $gettext('Import into %{name}', { name: c.name }) }}
            </oc-button>
          </li>
        </ul>
      </oc-drop>
      </template>
    </template>

    <no-content-message v-if="calendars.length === 0" icon="calendar" icon-fill-type="line">
      <template #message>
        <span data-testid="empty">{{ $gettext('You have no calendars yet') }}</span>
      </template>
      <template #callToAction>
        <oc-button variation="primary" appearance="filled" data-testid="empty-add" @click="dialog = { kind: 'create' }">
          {{ $gettext('Add calendar') }}
        </oc-button>
      </template>
    </no-content-message>
    <template v-else>
      <oc-progress v-if="loadingEvents" indeterminate size="small" class="gw-events-progress" />
      <p v-if="allHidden" class="oc-text-muted oc-mt-s oc-mb-s" data-testid="all-hidden">
        {{ $gettext('All calendars are hidden. Show a calendar in the list to see its events.') }}
      </p>
      <calendar-grid
        ref="grid"
        :events="shown"
        :initial-view="phone ? 'timeGridDay' : 'dayGridMonth'"
        :auto-height="phone"
        :compact="compact"
        :short-title="phone"
        :time-zone="settings.timeZone"
        :first-day="settings.firstDay"
        :week-numbers="settings.weekNumbers"
        @range="onRange"
        @view="(state: ViewState) => (view = state)"
        @select="onSelect"
        @open="openEvent"
      />
    </template>
    <input
      ref="fileInput"
      type="file"
      class="oc-invisible-sr"
      accept=".ics,text/calendar"
      tabindex="-1"
      aria-hidden="true"
      data-testid="calendar-import-file"
      @change="fileChosen"
    />
    <settings-dialog
      v-if="dialog?.kind === 'settings'"
      :settings="settings"
      :browser-zone="localZone()"
      @close="dialog = undefined"
      @save="saveSettings"
    />
    <calendar-dialog
      v-else-if="dialog?.kind === 'create'"
      :title="$gettext('Add calendar')"
      @close="dialog = undefined"
      @save="create"
    />
    <calendar-dialog
      v-else-if="dialog?.kind === 'edit'"
      :title="$gettext('Edit calendar')"
      :calendar="dialog.calendar"
      @close="dialog = undefined"
      @save="update"
    />
    <share-dialog
      v-else-if="dialog?.kind === 'share'"
      kind="calendar"
      :collection-id="dialog.calendar.id"
      :name="dialog.calendar.name"
      @close="dialog = undefined"
    />
    <event-dialog
      v-else-if="dialog?.kind === 'event-create'"
      :title="$gettext('New event')"
      :form="dialog.form"
      :calendars="writable"
      @close="dialog = undefined"
      @save="createEvent"
    />
    <import-report-dialog
      v-else-if="dialog?.kind === 'import-report'"
      :name="dialog.calendar.name"
      :report="dialog.report"
      @close="dialog = undefined"
    />
    <scope-dialog
      v-else-if="dialog?.kind === 'event-scope'"
      :remove="dialog.purpose === 'delete'"
      @close="dialog = undefined"
      @choose="chooseScope"
    />
    <event-dialog
      v-else-if="dialog?.kind === 'event-edit'"
      :title="editTitle(dialog)"
      :hide-recurrence="dialog.scope === 'this'"
      :form="dialog.form"
      :calendars="writable"
      existing
      @close="dialog = undefined"
      @save="changeEvent"
    />
    <confirm-dialog
      v-else-if="dialog?.kind === 'event-delete'"
      :title="deleteTitle(dialog.scope)"
      :message="deleteMessage(dialog.scope, dialog.event)"
      :confirm-label="$gettext('Delete')"
      @cancel="dialog = undefined"
      @confirm="deleteEvent"
    />
    <confirm-dialog
      v-else-if="dialog?.kind === 'delete'"
      :title="$gettext('Delete calendar')"
      :message="
        $gettext('Delete %{name} and all of its events? This cannot be undone.', {
          name: dialog.calendar.name
        })
      "
      :confirm-label="$gettext('Delete')"
      @cancel="dialog = undefined"
      @confirm="remove"
    />
  </app-shell>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useGettext } from 'vue3-gettext'
import AppShell from '../components/AppShell.vue'
import CalendarGrid from '../calendar/CalendarGrid.vue'
import CalendarDialog from '../calendar/CalendarDialog.vue'
import CalendarList from '../calendar/CalendarList.vue'
import CalendarSteps from '../calendar/CalendarSteps.vue'
import type { ViewState, ViewType } from '../calendar/view'
import { NoContentMessage, SideBarEventTopics, eventBus, useSideBar } from '@ownclouders/web-pkg'
import EventDetails from '../calendar/EventDetails.vue'
import SettingsDialog from '../calendar/SettingsDialog.vue'
import ImportReportDialog from '../calendar/ImportReportDialog.vue'
import { fileNameFor, saveBlob } from '../components/download'
import ScopeDialog from '../calendar/ScopeDialog.vue'
import EventDialog from '../calendar/EventDialog.vue'
import ShareDialog from '../sharing/ShareDialog.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import { useApi } from '../composables/useApi'
import { calendarIdOf, toCalendarEvent } from '../calendar/mapping'
import { useHiddenCalendars } from '../calendar/visibility'
import { blankForm, formFromEvent } from '../calendar/eventForm'
import { localZone } from '../calendar/zones'
import { useCalendarSettings, type CalendarSettings } from '../calendar/settings'
import type { CalendarInput, EventInput, GraphCalendar, GraphEvent, ImportReport } from '../api/types'
import type { EventForm } from '../calendar/eventForm'

defineOptions({ name: 'CalendarPage' })

const { $gettext } = useGettext()
const { run } = useApi()
const { hidden, isHidden, toggle } = useHiddenCalendars()
const { settings, update: updateSettings, zone } = useCalendarSettings()

const calendars = ref<GraphCalendar[]>([])
const grid = ref<InstanceType<typeof CalendarGrid>>()

// Below 640px the page opens on the day and the view buttons fold into a menu.
const phoneQuery = window.matchMedia('(max-width: 639px)')
const phone = ref(phoneQuery.matches)
const onPhoneChange = () => (phone.value = phoneQuery.matches)
// Below 960px the month shows dots instead of titles.
const compactQuery = window.matchMedia('(max-width: 959px)')
const compact = ref(compactQuery.matches)
const onCompactChange = () => (compact.value = compactQuery.matches)

const views = computed<{ type: ViewType; label: string }[]>(() => [
  { type: 'dayGridMonth', label: $gettext('Month') },
  { type: 'timeGridWeek', label: $gettext('Week') },
  { type: 'timeGridDay', label: $gettext('Day') }
])
// The toolbar shows what the grid reports. Until it has, the page is a month.
const view = ref<ViewState>({ type: phone.value ? 'timeGridDay' : 'dayGridMonth', title: '', isToday: true })
const currentViewLabel = computed(() => views.value.find((v) => v.type === view.value.type)?.label ?? '')
const events = ref<GraphEvent[]>([])
const loading = ref(true)
const loadingEvents = ref(false)
type Scope = 'single' | 'this' | 'all'
const dialog = ref<
  | { kind: 'settings' }
  | { kind: 'import-report'; calendar: GraphCalendar; report: ImportReport }
  | { kind: 'create' }
  | { kind: 'edit'; calendar: GraphCalendar }
  | { kind: 'delete'; calendar: GraphCalendar }
  | { kind: 'share'; calendar: GraphCalendar }
  | { kind: 'event-create'; form: EventForm }
  | { kind: 'event-scope'; occurrence: GraphEvent; series: GraphEvent; purpose: 'edit' | 'delete' }
  | {
      kind: 'event-edit'
      event: GraphEvent
      form: EventForm
      // Whether the event stands alone, is one occurrence of a series, or is the series.
      scope: Scope
    }
  | { kind: 'event-delete'; event: GraphEvent; scope: Scope }
>()

const allHidden = computed(
  () => calendars.value.length > 0 && calendars.value.every((c) => isHidden(c.id))
)

const byId = computed(() => new Map(calendars.value.map((c) => [c.id, c])))
const shown = computed(() =>
  events.value
    .filter((e) => byId.value.has(calendarIdOf(e.id)) && !isHidden(calendarIdOf(e.id)))
    .map((e) => ({
      ...toCalendarEvent(e, byId.value, $gettext('(No title)')),
      classNames: e.id === selection.value?.event.id ? ['gw-event-selected'] : []
    }))
)

// The grid reports the range it draws, and the events are read for exactly
// that range. A slow answer for a range that was left meanwhile is dropped.
let range: { start: string; end: string } | undefined
async function loadEvents() {
  if (!range) {
    return
  }
  const asked = range
  loadingEvents.value = true
  const loaded = await run($gettext('Could not load the events'), (c) => c.calendarView({ ...asked, zone: zone() }))
  if (range === asked) {
    loadingEvents.value = false
    if (loaded) {
      events.value = loaded
      refreshSelection()
    }
  }
}

function onRange(start: string, end: string) {
  range = { start, end }
  return loadEvents()
}

// writable lists the calendars an event can be created in, the default one
// first.
const writable = computed(() =>
  calendars.value
    .filter((c) => c.canEdit)
    .sort((a, b) => Number(b.isDefaultCalendar) - Number(a.isDefaultCalendar))
)

function newEvent(selection?: { start: string; end: string; allDay: boolean }) {
  const calendar = writable.value[0]
  if (!calendar) {
    return
  }
  const start = selection?.start ?? nextHour()
  dialog.value = {
    kind: 'event-create',
    form: blankForm({
      calendarId: calendar.id,
      zone: zone(),
      start,
      end: selection?.end,
      allDay: selection?.allDay ?? false
    })
  }
}

function nextHour(): string {
  const d = new Date()
  d.setHours(d.getHours() + 1, 0, 0, 0)
  return d.toISOString()
}

function onSelect(start: string, end: string, allDay: boolean) {
  if (lastClick && Date.now() - lastClick.at < doubleClickMs) {
    return
  }
  newEvent({ start, end, allDay })
}

function editDialog(event: GraphEvent, scope: Scope) {
  dialog.value = { kind: 'event-edit', event, form: formFromEvent(event), scope }
}

// What the sidebar shows is the event that was clicked. Its series is read
// along with it when it is an occurrence, for the pattern and to know whether
// the editor can change it.
const selection = ref<{ event: GraphEvent; series?: GraphEvent }>()
const { isSideBarOpen } = useSideBar()
const isOccurrence = (e: GraphEvent) => e.type !== 'singleInstance' && !!e.seriesMasterId
const selectedCalendar = computed(() =>
  selection.value ? byId.value.get(calendarIdOf(selection.value.event.id)) : undefined
)
const unsupportedPattern = computed(
  () => selection.value?.series?.['groupware.recurrenceEditable'] === false
)
const canChange = computed(() => !!selectedCalendar.value?.canEdit && !unsupportedPattern.value)
const notice = computed(() => {
  if (selection.value && !selectedCalendar.value?.canEdit) {
    return $gettext('This calendar is shared with you as read only.')
  }
  return unsupportedPattern.value
    ? $gettext(
        'This event repeats in a way the editor cannot show, so it cannot be changed here. Use a calendar app.'
      )
    : ''
})

let seriesRequest: Promise<GraphEvent | undefined> | undefined
function loadSeries(): Promise<GraphEvent | undefined> {
  const current = selection.value
  if (!current || !isOccurrence(current.event)) {
    return Promise.resolve(undefined)
  }
  if (current.series) {
    return Promise.resolve(current.series)
  }
  const id = current.event.id
  const masterId = current.event.seriesMasterId!
  seriesRequest = run($gettext('Could not load the event'), (c) => c.getEvent(masterId, zone())).then(
    (series) => {
      if (series && selection.value?.event.id === id) {
        selection.value = { ...selection.value, series }
      }
      return series
    }
  )
  return seriesRequest
}

function selectEvent(id: string) {
  const event = events.value.find((e) => e.id === id)
  if (!event) {
    return
  }
  selection.value = { event }
  eventBus.publish(SideBarEventTopics.open)
  void loadSeries()
}

function clearSelection() {
  selection.value = undefined
  eventBus.publish(SideBarEventTopics.close)
}

// A closed sidebar leaves nothing selected.
watch(isSideBarOpen, (open) => {
  if (!open) {
    selection.value = undefined
  }
})

// After the events are read again the selection shows the new state of its
// event, or goes when the event is gone.
function refreshSelection() {
  const current = selection.value
  if (!current) {
    return
  }
  const fresh = events.value.find((e) => e.id === current.event.id)
  if (!fresh) {
    clearSelection()
    return
  }
  selection.value = { event: fresh }
  void loadSeries()
}

// On a narrow window the sidebar covers the page and traps the focus, so a
// dialog opened from it would find no room: the sidebar closes first.
async function present(next: NonNullable<typeof dialog.value>) {
  if (compact.value && isSideBarOpen.value) {
    eventBus.publish(SideBarEventTopics.close)
    await nextTick()
  }
  dialog.value = next
}

async function startEdit() {
  const current = selection.value
  if (!current || !canChange.value) {
    return
  }
  if (!isOccurrence(current.event)) {
    await present({ kind: 'event-edit', event: current.event, form: formFromEvent(current.event), scope: 'single' })
    return
  }
  const series = current.series ?? (await loadSeries())
  if (series && series['groupware.recurrenceEditable'] !== false) {
    await present({ kind: 'event-scope', occurrence: current.event, series, purpose: 'edit' })
  }
}

async function startDelete() {
  const current = selection.value
  if (!current || !canChange.value) {
    return
  }
  if (!isOccurrence(current.event)) {
    await present({ kind: 'event-delete', event: current.event, scope: 'single' })
    return
  }
  const series = current.series ?? (await loadSeries())
  if (series) {
    await present({ kind: 'event-scope', occurrence: current.event, series, purpose: 'delete' })
  }
}

// Two clicks on the same event in quick succession go straight to the editor.
// The grid draws its events again when one is selected, so the browser's own
// double click event does not reach it; the clicks are timed here. The grid
// can also read the second click as a click on the day behind, which would
// open a new event, so a selection that follows an event click is ignored.
const doubleClickMs = 500
let lastClick: { id: string; at: number } | undefined
function openEvent(id: string) {
  const now = Date.now()
  const again = lastClick?.id === id && now - lastClick.at < doubleClickMs
  lastClick = { id, at: now }
  selectEvent(id)
  if (again) {
    void startEdit()
  }
}

const panels = computed(() => [
  {
    name: 'details',
    icon: 'information',
    title: () => $gettext('Details'),
    isRoot: () => true,
    isVisible: () => true,
    component: EventDetails,
    componentAttrs: () => ({
      event: selection.value?.event,
      calendar: selectedCalendar.value,
      series: selection.value?.series,
      canEdit: canChange.value,
      notice: notice.value,
      noticeIcon: selectedCalendar.value?.canEdit ? 'information' : 'lock',
      onEdit: startEdit,
      onRemove: startDelete
    })
  }
])

function chooseScope(scope: 'this' | 'all') {
  if (dialog.value?.kind !== 'event-scope') {
    return
  }
  const { occurrence, series, purpose } = dialog.value
  const event = scope === 'this' ? occurrence : series
  if (purpose === 'delete') {
    dialog.value = { kind: 'event-delete', event, scope }
    return
  }
  editDialog(event, scope)
}

function editTitle(d: { scope: Scope }) {
  return { single: $gettext('Edit event'), this: $gettext('Edit this event'), all: $gettext('Edit all events') }[d.scope]
}

function deleteTitle(scope: Scope) {
  return scope === 'all' ? $gettext('Delete all events') : scope === 'this' ? $gettext('Delete this event') : $gettext('Delete event')
}

function deleteMessage(scope: Scope, event: GraphEvent) {
  if (scope === 'all') {
    return $gettext('Delete every event of this series? This cannot be undone.')
  }
  if (scope === 'this') {
    return $gettext('Delete only this event? The rest of the series stays.')
  }
  return $gettext('Delete %{name}? This cannot be undone.', { name: event.subject || $gettext('this event') })
}

async function createEvent(save: { calendarId: string; input: EventInput }) {
  const created = await run($gettext('Could not create the event'), (c) =>
    c.createEvent(save.input, save.calendarId)
  )
  if (created) {
    dialog.value = undefined
    await loadEvents()
  }
}

async function changeEvent(save: { input: EventInput }) {
  if (dialog.value?.kind !== 'event-edit') {
    return
  }
  const event = dialog.value.event
  const changed = await run($gettext('Could not change the event'), (c) =>
    c.updateEvent(event.id, save.input, event['@odata.etag'])
  )
  if (changed) {
    dialog.value = undefined
    await loadEvents()
  }
}

async function deleteEvent() {
  if (dialog.value?.kind !== 'event-delete') {
    return
  }
  const event = dialog.value.event
  const done = await run($gettext('Could not delete the event'), async (c) => {
    await c.deleteEvent(event.id, event['@odata.etag'])
    return true
  })
  if (done) {
    dialog.value = undefined
    clearSelection()
    await loadEvents()
  }
}

function saveSettings(next: CalendarSettings) {
  updateSettings(next)
  dialog.value = undefined
  // The times the server sends depend on the zone.
  return loadEvents()
}

// One hidden file input serves every calendar; the one asked for is kept until
// a file is chosen.
const fileInput = ref<HTMLInputElement>()
let importTarget: GraphCalendar | undefined

function chooseFile(calendar: GraphCalendar) {
  importTarget = calendar
  fileInput.value?.click()
}

function fileChosen() {
  const input = fileInput.value
  const file = input?.files?.[0]
  if (importTarget && file) {
    importFile(importTarget, file)
  }
  if (input) {
    input.value = ''
  }
}

async function importFile(calendar: GraphCalendar, file: File) {
  const report = await run($gettext('Could not import the file'), (c) => c.importCalendar(calendar.id, file))
  if (report) {
    dialog.value = { kind: 'import-report', calendar, report }
    await loadEvents()
  }
}

async function exportCalendar(calendar: GraphCalendar) {
  const blob = await run($gettext('Could not export the calendar'), (c) => c.exportCalendar(calendar.id))
  if (blob) {
    saveBlob(blob, fileNameFor(calendar.name, 'ics'))
  }
}

async function create(input: CalendarInput) {
  const created = await run($gettext('Could not create the calendar'), (c) => c.createCalendar(input))
  if (created) {
    calendars.value = [...calendars.value, created]
    dialog.value = undefined
  }
}

async function update(input: CalendarInput) {
  const calendar = dialog.value?.kind === 'edit' ? dialog.value.calendar : undefined
  if (!calendar) {
    return
  }
  const changed = await run($gettext('Could not change the calendar'), (c) =>
    c.updateCalendar(calendar.id, input)
  )
  if (changed) {
    calendars.value = calendars.value.map((c) => (c.id === calendar.id ? changed : c))
    dialog.value = undefined
  }
}

async function remove() {
  const calendar = dialog.value?.kind === 'delete' ? dialog.value.calendar : undefined
  if (!calendar) {
    return
  }
  const done = await run($gettext('Could not delete the calendar'), async (c) => {
    await c.deleteCalendar(calendar.id)
    return true
  })
  if (done) {
    calendars.value = calendars.value.filter((c) => c.id !== calendar.id)
    dialog.value = undefined
  }
}

// Shortcuts for the toolbar, while the focus is not in a field.
function onKey(event: KeyboardEvent) {
  const target = event.target instanceof Element ? event.target : null
  if (
    event.ctrlKey ||
    event.metaKey ||
    event.altKey ||
    dialog.value ||
    target?.closest('input, textarea, select, [contenteditable="true"], [role="dialog"]')
  ) {
    return
  }
  const actions: Record<string, () => void> = {
    t: () => grid.value?.today(),
    n: () => grid.value?.next(),
    p: () => grid.value?.prev(),
    m: () => grid.value?.changeView('dayGridMonth'),
    w: () => grid.value?.changeView('timeGridWeek'),
    d: () => grid.value?.changeView('timeGridDay'),
    c: () => newEvent()
  }
  const action = actions[event.key]
  if (action && calendars.value.length > 0) {
    event.preventDefault()
    action()
  }
}

onMounted(async () => {
  phoneQuery.addEventListener('change', onPhoneChange)
  compactQuery.addEventListener('change', onCompactChange)
  document.addEventListener('keydown', onKey)
  calendars.value = (await run($gettext('Could not load the calendars'), (c) => c.listCalendars())) ?? []
  loading.value = false
})

onBeforeUnmount(() => {
  phoneQuery.removeEventListener('change', onPhoneChange)
  compactQuery.removeEventListener('change', onCompactChange)
  document.removeEventListener('keydown', onKey)
})
</script>

<style>
.gw-spacer {
  flex: 1 1 auto;
}
.gw-events-progress {
  flex: none;
  margin-bottom: var(--oc-space-xsmall);
}
</style>
