// Answers the media queries of the window with a function of the query, so a
// test can be a wide window or a phone. It returns a function that puts the
// original back.
export function mockMedia(matches: (query: string) => boolean = () => false): () => void {
  const original = window.matchMedia
  window.matchMedia = ((query: string) => ({
    matches: matches(query),
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {}
  })) as unknown as typeof window.matchMedia
  return () => {
    window.matchMedia = original
  }
}
