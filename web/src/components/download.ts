// fileNameFor makes a file name from the name of a calendar or address book,
// without the characters file systems refuse.
export function fileNameFor(name: string, extension: string): string {
  const clean = name.replace(/[\\/:*?"<>|\u0000-\u001f]+/g, ' ').replace(/\s+/g, ' ').trim()
  return `${clean || 'export'}.${extension}`
}

// saveBlob hands a file to the browser's download, through a link that is
// clicked and removed again.
export function saveBlob(blob: Blob, fileName: string) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = fileName
  document.body.appendChild(link)
  try {
    link.click()
  } finally {
    link.remove()
    URL.revokeObjectURL(url)
  }
}
