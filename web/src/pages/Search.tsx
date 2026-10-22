import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, type BookFound, type TitleResult } from '../api'
import AddDialog, { type AddTarget } from '../components/AddDialog'
import { BookAddDialog, BookCard } from '../components/BookDiscover'
import Icon from '../components/Icon'
import PosterCard from '../components/PosterCard'
import { capitalizeFirst } from '../components/SearchBox'
import { resultState } from '../components/state'
import { useModules } from '../ModulesContext'
import { booksTabLabel, resultsLink, resultsParams, resultsTab, splitBooks } from '../searchTabs'
import { t, tc } from '../i18n'
import KindGlyph from '../components/KindGlyph'
import '../search.css'

// Search the movie and TV databases as you type, like Radarr's and Sonarr's
// "Add New": pick a title, choose how to add it, and Mediarium then searches
// the indexers for releases of that title. Titles you already own are listed
// first and say where they stand. With ebooks or audiobooks switched on, books
// have a tab of their own, found the way the search box finds them.
export default function Search() {
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const { on } = useModules()
  const titlesOn = on('movies') || on('tv')
  const booksOn = on('ebooks') || on('audiobooks')
  const tab = resultsTab(params.get('tab'), titlesOn, booksOn)
  const urlQ = params.get('q') ?? ''
  const [q, setQ] = useState(urlQ)
  const [results, setResults] = useState<TitleResult[] | null>(null)
  const [error, setError] = useState('')
  const [adding, setAdding] = useState<AddTarget | null>(null)
  const [bookHits, setBookHits] = useState<BookFound[] | null>(null)
  const [bookError, setBookError] = useState('')
  const [addingBook, setAddingBook] = useState<BookFound | null>(null)
  const [booksAdded, setBooksAdded] = useState<Record<string, number>>({}) // added while on the page
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => inputRef.current?.focus(), [])
  // A new query from the top bar replaces what is typed here. The address is
  // updated with the trimmed text after each pause, so leave what is typed
  // alone when it only differs by spaces (or the word being typed loses its space).
  useEffect(() => setQ((cur) => (cur.trim() === urlQ ? cur : urlQ)), [urlQ])

  // Live: search shortly after the last keystroke, dropping stale answers.
  useEffect(() => {
    const term = q.trim()
    if (term.length < 2) {
      setResults(null)
      setError('')
      // Deleting down to one letter (or nothing) must not leave the old search in the address.
      if (term !== urlQ) setParams(resultsParams(tab, term), { replace: true })
      return
    }
    const ctl = new AbortController()
    const timer = setTimeout(() => {
      setError('')
      if (titlesOn) {
        api
          .discoverSearch(term, ctl.signal)
          .then(setResults)
          .catch((e) => {
            if (e instanceof DOMException && e.name === 'AbortError') return
            setError(e instanceof Error ? e.message : String(e))
          })
      } else {
        setResults([])
      }
      setParams(resultsParams(tab, term), { replace: true })
    }, 250)
    return () => {
      clearTimeout(timer)
      ctl.abort()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q])

  // Books come from Open Library, a little after the titles, whichever tab is
  // open, so the Books tab can say how many it found.
  useEffect(() => {
    const term = q.trim()
    if (!booksOn || term.length < 2) {
      setBookHits(null)
      setBookError('')
      return
    }
    let stale = false
    const timer = setTimeout(() => {
      api
        .searchBooks(term)
        .then((r) => {
          if (stale) return
          setBookHits(r)
          setBookError('')
        })
        .catch((e) => {
          if (stale) return
          setBookHits([])
          setBookError(e instanceof Error ? e.message : String(e))
        })
    }, 450)
    return () => {
      stale = true
      clearTimeout(timer)
    }
  }, [q, booksOn])

  // A book added from a card on this page moves up to "In your library".
  useEffect(() => {
    const onAdded = (e: Event) => {
      const d = (e as CustomEvent<{ key: string; id: number }>).detail
      setBooksAdded((o) => ({ ...o, [d.key]: d.id }))
    }
    window.addEventListener('mediarium:book-added', onAdded)
    return () => window.removeEventListener('mediarium:book-added', onAdded)
  }, [])

  const open = (r: TitleResult) => navigate(r.kind === 'movie' ? `/title/${r.tmdbId}` : `/series/${r.libraryId}`)

  const grid = (list: TitleResult[]) => (
    <div className="poster-grid">
      {list.map((r) => (
        <PosterCard
          key={`${r.kind}-${r.tmdbId}`}
          to={r.inLibrary ? (r.kind === 'movie' ? `/title/${r.tmdbId}` : `/series/${r.libraryId}`) : r.kind === 'movie' ? `/title/${r.tmdbId}` : `/show/${r.tmdbId}`}
          poster={r.posterUrl}
          title={r.title}
          meta={[r.year || null, r.kind === 'tv' ? t('TV show') : t('Movie')].filter(Boolean).join(' · ')}
          state={resultState(r)}
          kind={r.kind}
          genres={r.genres}
          rating={r.rating}
          footer={
            <div className="pcard-footer">
              {r.inLibrary ? (
                <button className="btn-sm btn-with-icon" onClick={() => open(r)}>
                  <Icon name="open" size={15} /> {t('Open')}
                </button>
              ) : (
                <button
                  className="primary btn-sm btn-with-icon"
                  onClick={() => setAdding({ kind: r.kind, tmdbId: r.tmdbId, title: r.title, year: r.year, posterUrl: r.posterUrl, overview: r.overview })}
                >
                  <Icon name="plus" size={15} /> {t('Add')}
                </button>
              )}
            </div>
          }
        />
      ))}
    </div>
  )

  const owned = results?.filter((r) => r.inLibrary) ?? []
  const fresh = results?.filter((r) => !r.inLibrary) ?? []
  const books = splitBooks(bookHits ?? [], booksAdded)
  const onBooks = tab === 'books'
  const term = q.trim()

  return (
    <div className="search-page">
      <div className="page-header">
        <h1>{tc('noun', 'Search')}</h1>
      </div>
      <nav className="search-modes" aria-label={tc('noun', 'Search')}>
        {titlesOn && (
          <Link to={resultsLink('titles', term)} className={onBooks ? undefined : 'active'} aria-current={onBooks ? undefined : 'page'}>
            <Icon name="film" size={15} /> {t('Movies and shows')}
          </Link>
        )}
        {booksOn && (
          <Link to={resultsLink('books', term)} className={onBooks ? 'active' : undefined} aria-current={onBooks ? 'page' : undefined}>
            <Icon name="book" size={15} /> {booksTabLabel(bookHits === null ? null : bookHits.length)}
          </Link>
        )}
        <Link to="/search/releases">
          <Icon name="list" size={15} /> {t('Releases')}
        </Link>
      </nav>

      <div className="search-bar">
        <Icon name="search" size={22} />
        <input
          ref={inputRef}
          type="search"
          value={q}
          onChange={(e) => setQ(capitalizeFirst(e.target.value))}
          placeholder={onBooks ? t('Type a book title or an author…') : t('Type a movie or show name…')}
          aria-label={t('Search')}
        />
      </div>

      {onBooks && (
        <>
          {bookError && <p className="error-text">{bookError}</p>}
          {bookHits === null && term.length < 2 && (
            <div className="search-hint">
              <span className="search-hint-shapes" aria-hidden="true">
                <KindGlyph kind="book" size={30} variant="solid" />
                <KindGlyph kind="audiobook" size={30} variant="solid" />
              </span>
              <p>{t('Start typing to search books. Anything already in your library shows where it stands.')}</p>
            </div>
          )}
          {bookHits === null && term.length >= 2 && <div className="search-hint">{t('Searching…')}</div>}
          {bookHits !== null && bookHits.length === 0 && !bookError && <div className="search-hint">{t('Nothing found for “{q}”.', { q: term })}</div>}
          {books.owned.length > 0 && (
            <section style={{ marginBottom: 28 }}>
              <div className="rail-head">
                <h2>{t('In your library')}</h2>
                <span>{books.owned.length}</span>
              </div>
              <div className="poster-grid">
                {books.owned.map((b) => (
                  <BookCard key={b.key} book={b} onAdd={setAddingBook} />
                ))}
              </div>
            </section>
          )}
          {books.fresh.length > 0 && (
            <section>
              <div className="rail-head">
                <h2>{t('Add new')}</h2>
                <span>{books.fresh.length}</span>
              </div>
              <div className="poster-grid">
                {books.fresh.map((b) => (
                  <BookCard key={b.key} book={b} onAdd={setAddingBook} />
                ))}
              </div>
            </section>
          )}
          <BookAddDialog target={addingBook} onClose={() => setAddingBook(null)} />
        </>
      )}

      {!onBooks && error && <p className="error-text">{error}</p>}
      {!onBooks && results === null && !error && q.trim().length < 2 && (
        <div className="search-hint">
          <span className="search-hint-shapes" aria-hidden="true">
            <KindGlyph kind="movie" size={30} variant="solid" />
            <KindGlyph kind="tv" size={30} variant="solid" />
          </span>
          <p>{t('Start typing to search movies and TV shows. Anything already in your library shows where it stands.')}</p>
        </div>
      )}
      {!onBooks && results !== null && results.length === 0 && <div className="search-hint">{t('Nothing found for “{q}”.', { q: q.trim() })}</div>}
      {!onBooks && owned.length > 0 && (
        <section style={{ marginBottom: 28 }}>
          <div className="rail-head">
            <h2>{t('In your library')}</h2>
            <span>{owned.length}</span>
          </div>
          {grid(owned)}
        </section>
      )}
      {!onBooks && fresh.length > 0 && (
        <section>
          <div className="rail-head">
            <h2>{t('Add new')}</h2>
            <span>{fresh.length}</span>
          </div>
          {grid(fresh)}
        </section>
      )}

      {adding && (
        <AddDialog
          target={adding}
          onClose={() => setAdding(null)}
          onAdded={(id) => {
            const added = adding
            setAdding(null)
            navigate(added.kind === 'movie' ? `/title/${added.tmdbId}` : `/series/${id}`)
          }}
        />
      )}
    </div>
  )
}
