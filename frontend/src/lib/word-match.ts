/** True when every whitespace-separated word of `query` appears somewhere in
 * `haystack`, ignoring case. An empty query matches everything. */
export function matchesAllWords(haystack: string, query: string): boolean {
  const lower = haystack.toLowerCase()
  return query.toLowerCase().split(/\s+/).filter(Boolean).every((w) => lower.includes(w))
}
