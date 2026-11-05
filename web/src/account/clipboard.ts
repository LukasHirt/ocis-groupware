// copyText puts text on the clipboard and says whether it worked. The
// clipboard API needs a secure context and the user's permission, so a
// refusal is an answer, not an error.
export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    return false
  }
}
