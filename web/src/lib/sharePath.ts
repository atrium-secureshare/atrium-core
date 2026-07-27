// Relative-path helpers for share folder browsing. A sub-path is a plain "a/b/c"
// string; the share token stays the only access anchor, so sub-folders carry no id.
export function segments(path: string): string[] {
  return path.split('/').filter(Boolean)
}

export function joinPath(path: string, segment: string): string {
  return path ? `${path}/${segment}` : segment
}

export function parentPath(path: string): string {
  const parts = segments(path)
  parts.pop()
  return parts.join('/')
}

export function shareUrl(hash: string, path = ''): string {
  const base = `/share/${encodeURIComponent(hash)}`
  return path ? `${base}/${path}` : base
}
