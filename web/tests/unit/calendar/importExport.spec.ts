import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ImportReportDialog from '../../../src/calendar/ImportReportDialog.vue'
import { fileNameFor, saveBlob } from '../../../src/components/download'
import type { ImportReport } from '../../../src/api/types'
import report from '../../../../internal/graphapi/testdata/contract/import-report.json'

vi.mock('vue3-gettext', () => ({
  useGettext: () => ({
    $gettext: (s: string, vars?: Record<string, string>) =>
      s.replace(/%\{(\w+)\}/g, (_m, k: string) => vars?.[k] ?? '')
  })
}))

describe('ImportReportDialog', () => {
  it('sums up the import and lists each failure with its position and reason', () => {
    const wrapper = mount(ImportReportDialog, {
      props: { name: 'Work', report: report as ImportReport }
    })
    expect(wrapper.get('[data-testid="import-summary"]').text()).toBe(
      '3 found: 2 added, 0 replaced, 1 failed.'
    )
    const failures = wrapper.findAll('[data-testid="import-failure"]')
    expect(failures).toHaveLength(1)
    expect(failures[0].text()).toContain('2')
    expect(failures[0].text()).toContain('todo')
    expect(failures[0].text()).toContain('VTODO')
  })

  it('has no failure list when everything was stored', () => {
    const ok: ImportReport = {
      total: 1,
      created: 1,
      updated: 0,
      failed: 0,
      results: [{ index: 0, uid: 'a', name: 'a.ics', status: 'created' }]
    }
    const wrapper = mount(ImportReportDialog, { props: { name: 'Work', report: ok } })
    expect(wrapper.find('[data-testid="import-failure"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="import-summary"]').text()).toContain('1 added')
  })

  it('closes', async () => {
    const wrapper = mount(ImportReportDialog, { props: { name: 'Work', report: report as ImportReport } })
    await wrapper.get('[data-testid="dialog-confirm"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})

describe('fileNameFor', () => {
  it('makes a safe file name from a calendar name', () => {
    expect(fileNameFor('Work', 'ics')).toBe('Work.ics')
    expect(fileNameFor('a/b:c*?', 'ics')).toBe('a b c.ics')
    expect(fileNameFor('  ', 'vcf')).toBe('export.vcf')
  })
})

describe('saveBlob', () => {
  it('clicks a temporary link with the file name and frees the address', () => {
    const create = vi.fn(() => 'blob:x')
    const revoke = vi.fn()
    Object.assign(URL, { createObjectURL: create, revokeObjectURL: revoke })
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      expect(this.download).toBe('Work.ics')
      expect(this.href).toContain('blob:x')
    })
    saveBlob(new Blob(['x']), 'Work.ics')
    expect(click).toHaveBeenCalledTimes(1)
    expect(revoke).toHaveBeenCalledWith('blob:x')
    expect(document.querySelector('a[download]')).toBeNull()
    click.mockRestore()
  })
})
