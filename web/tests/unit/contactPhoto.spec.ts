import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import contact from '../../../internal/graphapi/testdata/contract/contact.json'
import type { GraphContact } from '../../src/api/types'
import { checkPhoto, maxPhotoBytes } from '../../src/contacts/photo'
import { initials } from '../../src/contacts/model'

vi.mock('vue3-gettext', () => ({
  useGettext: () => ({ $gettext: (s: string) => s })
}))

const getContactPhoto = vi.fn()
const setContactPhoto = vi.fn()
const deleteContactPhoto = vi.fn()
vi.mock('../../src/composables/useApi', () => {
  const client = {
    getContactPhoto: (...a: unknown[]) => getContactPhoto(...a),
    setContactPhoto: (...a: unknown[]) => setContactPhoto(...a),
    deleteContactPhoto: (...a: unknown[]) => deleteContactPhoto(...a)
  }
  return {
    useApi: () => ({
      client,
      run: async (_t: string, f: (c: unknown) => unknown) => {
        try {
          return await f(client)
        } catch {
          return undefined
        }
      }
    })
  }
})

import ContactPhoto from '../../src/contacts/ContactPhoto.vue'

const marie = contact as unknown as GraphContact
const file = (type: string, size = 10) => {
  const f = new File(['x'], 'p', { type })
  Object.defineProperty(f, 'size', { value: size })
  return f
}

describe('checkPhoto', () => {
  it('accepts JPEG and PNG up to the limit', () => {
    expect(checkPhoto(file('image/jpeg'))).toBeNull()
    expect(checkPhoto(file('image/png', maxPhotoBytes))).toBeNull()
  })

  it('refuses other types and large files', () => {
    expect(checkPhoto(file('image/gif'))).toBe('type')
    expect(checkPhoto(file('image/png', maxPhotoBytes + 1))).toBe('size')
  })
})

describe('initials', () => {
  it('takes the first letters of the first and last word', () => {
    expect(initials('Dr. Marie Salomea Curie')).toBe('DC')
    expect(initials('madonna')).toBe('M')
    expect(initials('')).toBe('')
  })
})

describe('the photo of a contact', () => {
  beforeEach(() => {
    getContactPhoto.mockReset()
    setContactPhoto.mockReset()
    deleteContactPhoto.mockReset()
    URL.createObjectURL = vi.fn(() => 'blob:photo')
    URL.revokeObjectURL = vi.fn()
  })
  const mountIt = (hasPhoto: boolean, canEdit = true) =>
    mount(ContactPhoto, {
      props: { contact: { ...marie, 'groupware.hasPhoto': hasPhoto }, name: 'Marie', canEdit },
      global: { mocks: { $gettext: (s: string) => s } }
    })

  it('shows the initials when the contact has no photo', async () => {
    const w = mountIt(false)
    await flushPromises()
    expect(getContactPhoto).not.toHaveBeenCalled()
    expect(w.find('[data-testid="photo-initials"]').text()).toBe('M')
    expect(w.find('[data-testid="photo-remove"]').exists()).toBe(false)
  })

  it('loads and shows the photo', async () => {
    getContactPhoto.mockResolvedValue(new Blob(['x'], { type: 'image/png' }))
    const w = mountIt(true)
    await flushPromises()
    expect(getContactPhoto).toHaveBeenCalledWith(marie.parentFolderId, marie.id)
    expect(w.find('img').attributes('src')).toBe('blob:photo')
  })

  it('uploads a chosen file and tells the page', async () => {
    setContactPhoto.mockResolvedValue({})
    const w = mountIt(false)
    const input = w.find('[data-testid="photo-input"]')
    Object.defineProperty(input.element, 'files', { value: [file('image/png')] })
    await input.trigger('change')
    await flushPromises()
    expect(setContactPhoto).toHaveBeenCalledWith(marie.parentFolderId, marie.id, expect.any(File))
    expect(w.emitted('changed')).toHaveLength(1)
  })

  it('does not send a file that is too large or of the wrong type', async () => {
    const w = mountIt(false)
    const input = w.find('[data-testid="photo-input"]')
    Object.defineProperty(input.element, 'files', { value: [file('image/gif')], configurable: true })
    await input.trigger('change')
    expect(w.find('[data-testid="photo-error"]').text()).toContain('JPEG or PNG')
    Object.defineProperty(input.element, 'files', {
      value: [file('image/png', maxPhotoBytes + 1)],
      configurable: true
    })
    await input.trigger('change')
    expect(w.find('[data-testid="photo-error"]').text()).toContain('1 MB')
    expect(setContactPhoto).not.toHaveBeenCalled()
  })

  it('removes the photo', async () => {
    getContactPhoto.mockResolvedValue(new Blob(['x'], { type: 'image/png' }))
    deleteContactPhoto.mockResolvedValue(undefined)
    const w = mountIt(true)
    await flushPromises()
    await w.find('[data-testid="photo-remove"]').trigger('click')
    await flushPromises()
    expect(deleteContactPhoto).toHaveBeenCalledWith(marie.parentFolderId, marie.id)
    expect(w.emitted('changed')).toHaveLength(1)
  })

  it('offers no changes on a read-only contact', async () => {
    const w = mountIt(true, false)
    await flushPromises()
    expect(w.find('[data-testid="photo-input"]').exists()).toBe(false)
    expect(w.find('[data-testid="photo-remove"]').exists()).toBe(false)
  })
})
