import { beforeEach, describe, expect, it } from 'vitest'
import { defaultSettings, loadSettings, saveSettings } from '../../../src/calendar/settings'

describe('calendar settings', () => {
  beforeEach(() => localStorage.clear())

  it('starts from the defaults: the browser zone, Monday, no week numbers', () => {
    expect(loadSettings('u1')).toEqual({ timeZone: '', firstDay: 1, weekNumbers: false })
    expect(defaultSettings).toEqual(loadSettings('u1'))
  })

  it('keeps the settings of each user apart', () => {
    saveSettings('u1', { timeZone: 'Europe/Prague', firstDay: 0, weekNumbers: true })
    expect(loadSettings('u1')).toEqual({ timeZone: 'Europe/Prague', firstDay: 0, weekNumbers: true })
    expect(loadSettings('u2')).toEqual(defaultSettings)
  })

  it('replaces a value that makes no sense with the default for it', () => {
    localStorage.setItem(
      'groupware.calendarSettings.u1',
      JSON.stringify({ timeZone: 'No/Such_Zone', firstDay: 9, weekNumbers: 'yes' })
    )
    expect(loadSettings('u1')).toEqual(defaultSettings)
  })

  it('ignores stored text that is not JSON', () => {
    localStorage.setItem('groupware.calendarSettings.u1', '{nope')
    expect(loadSettings('u1')).toEqual(defaultSettings)
  })

  it('does not fail when the browser refuses storage', () => {
    const get = Storage.prototype.getItem
    const set = Storage.prototype.setItem
    Storage.prototype.getItem = () => {
      throw new Error('denied')
    }
    Storage.prototype.setItem = () => {
      throw new Error('denied')
    }
    try {
      expect(loadSettings('u1')).toEqual(defaultSettings)
      expect(() => saveSettings('u1', defaultSettings)).not.toThrow()
    } finally {
      Storage.prototype.getItem = get
      Storage.prototype.setItem = set
    }
  })
})
