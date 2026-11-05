// The service takes JPEG and PNG images of at most 1 MB.
export const maxPhotoBytes = 1024 * 1024

export const photoTypes = ['image/jpeg', 'image/png']

// checkPhoto says why a file cannot be used as a contact photo, or null.
export function checkPhoto(file: File): 'type' | 'size' | null {
  if (!photoTypes.includes(file.type)) return 'type'
  if (file.size > maxPhotoBytes) return 'size'
  return null
}
