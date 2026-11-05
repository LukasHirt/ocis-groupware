import { expect, type Page, type TestInfo } from '@playwright/test'
import { appendFileSync, mkdirSync } from 'node:fs'
import { join } from 'node:path'

// What the screen tests assert on every screen they capture, kept apart from
// the tour itself so that each check reads on its own.

export interface Mode {
  name: 'light' | 'dark' | 'phone'
  colorScheme: 'light' | 'dark'
  width: number
  height: number
}

export const modes: Mode[] = [
  { name: 'light', colorScheme: 'light', width: 1440, height: 900 },
  { name: 'dark', colorScheme: 'dark', width: 1440, height: 900 },
  { name: 'phone', colorScheme: 'light', width: 390, height: 844 }
]

// Where the pictures go. The folder is inside Playwright's output directory,
// which CI uploads.
export const screensDir = 'test-results/screens'

// Problems of the whole run: errors the browser reported and requests that
// failed. Collected from the start, read at the end.
export function watchBrowser(page: Page): () => string[] {
  const problems: string[] = []
  const known = (url: string) => /\/(favicon\.ico)$/.test(url)
  page.on('pageerror', (err) => problems.push(`page error: ${err.message}`))
  page.on('console', (msg) => {
    if (msg.type() === 'error') problems.push(`console error: ${msg.text()} (${msg.location().url})`)
  })
  page.on('requestfailed', (req) => {
    const why = req.failure()?.errorText ?? ''
    // A request that the page itself cancelled when it moved on is not a failure.
    if (why === 'net::ERR_ABORTED' || known(req.url())) return
    problems.push(`request failed: ${req.method()} ${req.url()} ${why}`)
  })
  page.on('response', (res) => {
    if (res.status() >= 400 && !known(res.url())) {
      problems.push(`response ${res.status()}: ${res.request().method()} ${res.url()}`)
    }
  })
  return () => problems
}

// Elements the checks leave to oCIS Web: its top bar and navigation are not
// ours to restyle.
const webChrome = '#web-nav-sidebar, #oc-topbar, header, .oc-topbar'

// Findings that are tolerated, with the reason. A check only skips an element
// when it matches one of these and says so in its report.
export const tolerated = {
  // The colour swatches of the calendar dialog: the design system has no
  // colour picker.
  noDesignSystemCounterpart: '.gw-swatch-button, .gw-swatch-picker',
  // FullCalendar draws its own now-indicator, popover frame and week-number
  // cells; their colours come from the library's stylesheet.
  fullCalendarInternals: '.fc-popover-header, .fc-timegrid-now-indicator-arrow, .fc-timegrid-now-indicator-line'
}

export interface Finding {
  where: string
  detail: string
}

// pageScroll holds what the page says about its size.
export async function horizontalOverflow(page: Page): Promise<Finding[]> {
  return page.evaluate(() => {
    const out: { where: string; detail: string }[] = []
    const root = document.documentElement
    if (root.scrollWidth > root.clientWidth + 1) {
      out.push({ where: 'document', detail: `scrollWidth ${root.scrollWidth} > ${root.clientWidth}` })
    }
    const width = window.innerWidth
    // A dialog and the app itself must lie inside the window.
    for (const el of document.querySelectorAll('[role="dialog"], .gw-app, .gw-body, .gw-app-bar')) {
      const r = el.getBoundingClientRect()
      // Behind a sidebar that takes the whole width of a phone the page is squeezed to a sliver.
      if (r.width === 0 || (r.width < 100 && width < 640)) continue
      if (r.right > width + 1 || r.left < -1) {
        out.push({ where: describe(el), detail: `spans ${Math.round(r.left)} to ${Math.round(r.right)} of ${width}` })
      }
      if (el.scrollWidth > el.clientWidth + 1 && getComputedStyle(el).overflowX === 'visible') {
        out.push({ where: describe(el), detail: `content ${el.scrollWidth} wider than box ${el.clientWidth}` })
      }
    }
    return out

    function describe(el: Element) {
      const id = el.getAttribute('data-testid')
      return `${el.tagName.toLowerCase()}${id ? `[${id}]` : ''}.${[...el.classList].slice(0, 2).join('.')}`
    }
  })
}

// contentReachable scrolls every scrollable ancestor of the content (the open
// dialog when there is one, else the body of the page) to its end and checks
// that the last thing in it is then inside the window, so nothing is cut off.
export async function contentReachable(page: Page): Promise<Finding[]> {
  return page.evaluate(() => {
    const dialog = [...document.querySelectorAll('[role="dialog"]')].find((d) => d.getBoundingClientRect().height > 0)
    const root = dialog?.querySelector('.gw-modal-form') ?? dialog ?? document.querySelector('.gw-body')
    if (!root) return [{ where: 'screen', detail: 'no content element found' }]
    const scrollables: Element[] = []
    for (let el: Element | null = root; el; el = el.parentElement) {
      const s = getComputedStyle(el)
      if (/(auto|scroll)/.test(s.overflowY) && el.scrollHeight > el.clientHeight) scrollables.push(el)
    }
    const doc = document.scrollingElement
    scrollables.forEach((el) => (el.scrollTop = el.scrollHeight))
    if (doc) doc.scrollTop = doc.scrollHeight
    const children = [...root.children].filter((c) => c.getBoundingClientRect().height > 0)
    const last = children[children.length - 1]
    const out: { where: string; detail: string }[] = []
    if (last) {
      const bottom = last.getBoundingClientRect().bottom
      if (bottom > window.innerHeight + 1) {
        out.push({ where: 'content', detail: `end of the content at ${Math.round(bottom)}, window is ${window.innerHeight} high and nothing scrolls further` })
      }
    }
    scrollables.forEach((el) => (el.scrollTop = 0))
    if (doc) doc.scrollTop = 0
    return out
  })
}

// nativeControls lists buttons, inputs, selects and text areas that are not
// made by the design system. Its components give their element a class that
// starts with oc- (or vs__ for the select's search field).
export async function nativeControls(page: Page): Promise<Finding[]> {
  return page.evaluate(({ chrome, allowed }) => {
    const out: { where: string; detail: string }[] = []
    for (const el of document.querySelectorAll('button, input, select, textarea')) {
      if (el.closest(chrome)) continue
      const type = el.getAttribute('type')
      if (type === 'hidden' || type === 'file') continue
      // Web's skip link is its own.
      if (el.classList.contains('skip-button')) continue
      if (el.closest('.fc-popover, .fc-toolbar')) continue
      // The colour swatches of the calendar dialog have no counterpart in the design system.
      if (el.matches(allowed)) continue
      const classes = [...el.classList]
      if (classes.some((c) => c.startsWith('oc-') || c.startsWith('vs__'))) continue
      // Web's own elements and the design system's wrappers carry the class on a parent.
      if (el.closest('.oc-checkbox-wrapper, .oc-radio-wrapper, .oc-switch')) continue
      const rect = el.getBoundingClientRect()
      if (rect.width === 0 && rect.height === 0) continue
      out.push({
        where: `${el.tagName.toLowerCase()}${el.getAttribute('data-testid') ? `[${el.getAttribute('data-testid')}]` : ''}`,
        detail: `class="${el.className}" text="${(el.textContent ?? '').trim().slice(0, 30)}"`
      })
    }
    return out
  }, { chrome: webChrome, allowed: tolerated.noDesignSystemCounterpart })
}

// firstFocus presses Tab once from the dialog (or the app) and checks that the
// element that has the focus then shows it.
export async function focusIndicator(page: Page): Promise<Finding[]> {
  const dialog = page.locator('[role="dialog"]:visible').first()
  if ((await dialog.count()) > 0) {
    // The dialog takes the focus itself or hands it to a field or a button.
    const has = await dialog.evaluate((d) => d.contains(document.activeElement) && document.activeElement !== d)
    if (!has) await page.keyboard.press('Tab')
  } else {
    await page.evaluate(() => {
      const scope = document.querySelector('.gw-sidebar') ? '.gw-sidebar' : '.gw-app'
      const first = [...document.querySelectorAll<HTMLElement>(`${scope} button, ${scope} a[href], ${scope} input, ${scope} [tabindex="0"]`)].find(
        (e) => !e.hasAttribute('disabled') && e.getBoundingClientRect().width > 0 && !e.closest('[hidden]')
      )
      ;(document.activeElement as HTMLElement | null)?.blur()
      first?.focus()
    })
  }
  // A scripted focus after a mouse click does not count as keyboard focus.
  // A key press makes the next focus one, so the element takes the focus again.
  await page.keyboard.press('Shift')
  return page.evaluate(() => {
    const el = document.activeElement as HTMLElement | null
    if (!el || el === document.body) return [{ where: 'focus', detail: 'nothing has the focus' }]
    const where = `${el.tagName.toLowerCase()}${el.getAttribute('data-testid') ? `[${el.getAttribute('data-testid')}]` : ''}`
    // The design system's select and checkbox draw the focus on a wrapper, so the element and its
    // nearest ancestors are looked at together.
    const look = () => {
      const chain = [el, el.parentElement, el.parentElement?.parentElement].filter((e): e is HTMLElement => !!e)
      const styles = chain.map((e) => getComputedStyle(e))
      return {
        outline: styles.some((s) => s.outlineStyle !== 'none' && parseFloat(s.outlineWidth) > 0),
        shadow: styles.map((s) => s.boxShadow).join('|'),
        border: styles.map((s) => s.borderColor).join('|'),
        text: styles[0].outline
      }
    }
    el.blur()
    const off = look()
    el.focus()
    const on = look()
    // A ring, a shadow or a change of the border colour is a visible focus.
    if (on.outline || on.shadow !== off.shadow || on.border !== off.border) return []
    return [{ where, detail: `nothing changes when it has the focus (outline ${on.text}, shadow ${on.shadow}, border ${on.border})` }]
  })
}

// escapeCloses presses Escape while a dialog is open and says whether the
// dialog is gone.
export async function escapeCloses(page: Page, name: string): Promise<Finding[]> {
  const dialog = page.locator('[role="dialog"]:visible')
  if ((await dialog.count()) === 0) return [{ where: name, detail: 'no dialog was open' }]
  await page.keyboard.press('Escape')
  try {
    await expect(dialog).toHaveCount(0, { timeout: 3000 })
    return []
  } catch {
    return [{ where: name, detail: 'the dialog is still open after Escape' }]
  }
}

// contrast measures the text of the screen against its background. The ratio
// follows WCAG 2.x: 4.5 for normal text and 3 for large text (24px, or 19px
// bold). Backgrounds are composed from the element up through its ancestors;
// an element over an image or a gradient is skipped, as its colour cannot be
// read from the style.
export async function contrast(page: Page): Promise<{ failures: Finding[]; tolerated: Finding[] }> {
  return page.evaluate(
    ({ chrome, allowed }) => {
      const canvas = document.createElement('canvas')
      canvas.width = canvas.height = 1
      const ctx = canvas.getContext('2d', { willReadFrequently: true })!
      type RGBA = [number, number, number, number]
      const parse = (css: string): RGBA => {
        ctx.clearRect(0, 0, 1, 1)
        ctx.fillStyle = '#000'
        ctx.fillStyle = css
        ctx.fillRect(0, 0, 1, 1)
        const d = ctx.getImageData(0, 0, 1, 1).data
        return [d[0], d[1], d[2], d[3] / 255]
      }
      const over = (top: RGBA, bottom: RGBA): RGBA => {
        const a = top[3] + bottom[3] * (1 - top[3])
        if (a === 0) return [0, 0, 0, 0]
        const c = (i: number) => (top[i] * top[3] + bottom[i] * bottom[3] * (1 - top[3])) / a
        return [c(0), c(1), c(2), a]
      }
      const lum = ([r, g, b]: RGBA) => {
        const f = (v: number) => {
          const s = v / 255
          return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
        }
        return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b)
      }
      const page: RGBA = parse(getComputedStyle(document.body).backgroundColor)
      function background(el: Element): RGBA | null {
        const chain: Element[] = []
        for (let e: Element | null = el; e; e = e.parentElement) chain.push(e)
        let acc: RGBA = [0, 0, 0, 0]
        // Walk from the element up until the colour is opaque.
        for (const e of chain) {
          const s = getComputedStyle(e)
          if (s.backgroundImage !== 'none') return null
          acc = over(acc, parse(s.backgroundColor))
          if (acc[3] >= 0.999) return acc
        }
        return over(acc, page[3] > 0 ? page : [255, 255, 255, 1])
      }
      function opacity(el: Element) {
        let o = 1
        for (let e: Element | null = el; e; e = e.parentElement) o *= parseFloat(getComputedStyle(e).opacity)
        return o
      }
      const failures: { where: string; detail: string }[] = []
      const skipped: { where: string; detail: string }[] = []
      const seen = new Set<string>()
      const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT)
      for (let n = walker.nextNode(); n; n = walker.nextNode()) {
        const text = (n.textContent ?? '').trim()
        const el = n.parentElement
        if (!text || !el) continue
        if (el.closest(chrome) || el.closest('script, style, noscript, .oc-invisible, .oc-hidden-announcer, [aria-hidden="true"]')) continue
        const r = el.getBoundingClientRect()
        if (r.width === 0 || r.height === 0 || r.bottom < 0 || r.top > window.innerHeight) continue
        const s = getComputedStyle(el)
        if (s.visibility === 'hidden' || s.display === 'none') continue
        // Disabled controls are exempt in WCAG; text that a native tooltip or placeholder draws is not text here.
        if (el.closest('[disabled], [aria-disabled="true"]')) continue
        const bg = background(el)
        if (!bg) continue
        const op = opacity(el)
        const fg0 = parse(s.color)
        const fg = over([fg0[0], fg0[1], fg0[2], fg0[3] * op], bg)
        const l1 = lum(fg)
        const l2 = lum(bg)
        const ratio = (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05)
        const size = parseFloat(s.fontSize)
        const bold = parseInt(s.fontWeight, 10) >= 700
        const large = size >= 24 || (size >= 18.66 && bold)
        const need = large ? 3 : 4.5
        if (ratio >= need) continue
        const id = `${el.tagName}.${[...el.classList].join('.')}|${text.slice(0, 20)}`
        if (seen.has(id)) continue
        seen.add(id)
        const item = {
          where: `${el.tagName.toLowerCase()}.${[...el.classList].slice(0, 3).join('.')}`,
          detail: `"${text.slice(0, 40)}" ${ratio.toFixed(2)}:1, needs ${need} (text ${s.color}, ${Math.round(size)}px)`
        }
        if (el.closest(allowed)) skipped.push(item)
        else failures.push(item)
      }
      return { failures, tolerated: skipped }
    },
    { chrome: webChrome, allowed: tolerated.fullCalendarInternals }
  )
}

// capture saves the screenshot of the window and runs the checks. Findings
// are soft failures, so one run lists every defect instead of stopping at the
// first.
export async function capture(
  page: Page,
  info: TestInfo,
  mode: Mode,
  name: string,
  options: { dialog?: boolean; skipFocus?: boolean } = {}
) {
  const dir = join(screensDir, mode.name)
  mkdirSync(dir, { recursive: true })
  // Let animations and fonts settle before the picture.
  await page.waitForTimeout(300)
  await page.screenshot({ path: join(dir, `${name}.png`), animations: 'disabled' })

  const report = (check: string, findings: Finding[]) => {
    for (const f of findings) appendFileSync(join(screensDir, 'findings.log'), `${mode.name}/${name}/${check}: ${f.where}: ${f.detail}\n`)
    for (const f of findings) info.annotations.push({ type: `${mode.name}/${name}/${check}`, description: `${f.where}: ${f.detail}` })
    expect.soft(findings, `${mode.name}/${name}: ${check}`).toEqual([])
  }
  report('overflow', await horizontalOverflow(page))
  report('scroll', await contentReachable(page))
  report('native controls', await nativeControls(page))
  // Contrast first: moving the focus can open a select's list, which is not part of the screen.
  const c = await contrast(page)
  report('contrast', c.failures)
  for (const f of c.tolerated) info.annotations.push({ type: `${mode.name}/${name}/tolerated-contrast`, description: `${f.where}: ${f.detail}` })
  if (!options.skipFocus) {
    report('focus', await focusIndicator(page))
    // A select opens its list when it takes the focus; Escape would only close that.
    if (await page.locator('.vs--open').count()) await page.keyboard.press('Escape')
  }
}
