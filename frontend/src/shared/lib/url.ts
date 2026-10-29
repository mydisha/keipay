/** Builds `path?query`, leaving out unset values, for `<a href download>` links. */
export function withQuery(path: string, query: Record<string, string | boolean | undefined>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined) params.set(key, String(value));
  }
  const text = params.toString();
  return text ? `${path}?${text}` : path;
}
