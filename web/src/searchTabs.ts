import type { BookFound } from './api'
import { t } from './i18n.ts'

// The tabs of the search results page. "Releases" is its own page; these two
// share /search and are told apart by the tab in the address.
export type ResultsTab = 'titles' | 'books'

// resultsTab is the tab to show: the one in the address when it can be shown,
// else the titles, or the books when movies and shows are both switched off.
export function resultsTab(param: string | null, titlesOn: boolean, booksOn: boolean): ResultsTab {
  if (param === 'books' && booksOn) return 'books'
  if (param === 'titles' && titlesOn) return 'titles'
  return titlesOn || !booksOn ? 'titles' : 'books'
}

// resultsLink is the address of a results tab for a search term. The titles
// are the default tab, so they have no tab in the address.
export function resultsLink(tab: ResultsTab, q: string): string {
  const p = new URLSearchParams()
  const term = q.trim()
  if (term) p.set('q', term)
  if (tab === 'books') p.set('tab', 'books')
  const s = p.toString()
  return s ? `/search?${s}` : '/search'
}

// resultsParams is the query part of the address for the term typed on a tab.
export function resultsParams(tab: ResultsTab, q: string): Record<string, string> {
  const term = q.trim()
  const out: Record<string, string> = {}
  if (term) out.q = term
  if (tab === 'books') out.tab = 'books'
  return out
}

// booksTabLabel is the label of the Books tab: with the number of books found
// once the search has answered.
export function booksTabLabel(count: number | null): string {
  return count === null ? t('Books') : t('Books ({n})', { n: count })
}

// splitBooks puts the books already in the library first, like the titles.
export function splitBooks(list: BookFound[], added: Record<string, number> = {}): { owned: BookFound[]; fresh: BookFound[] } {
  const marked = list.map((b) => (added[b.key] ? { ...b, libraryId: added[b.key] } : b))
  return { owned: marked.filter((b) => !!b.libraryId), fresh: marked.filter((b) => !b.libraryId) }
}
