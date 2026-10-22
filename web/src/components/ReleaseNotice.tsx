import { Link } from 'react-router-dom'
import { bookReleaseHint, releaseHint, skippedHint, type Unanswered } from '../releaseHint'
import { st, t } from '../i18n'

// The line about the indexers next to a release list: says why there is
// nothing to show, or that some sources did not answer. For a book search,
// skipped names the indexers that were left out because they don't do books.
export default function ReleaseNotice({
  sources,
  unanswered,
  warnings,
  results,
  className,
  skipped,
}: {
  sources: number
  unanswered: Unanswered[]
  warnings?: string[]
  results: number
  className?: string
  skipped?: string[]
}) {
  const h = skipped ? bookReleaseHint(sources, unanswered, results, skipped) : releaseHint(sources, unanswered, results)
  const left = skipped ? skippedHint(skipped) : null
  if (!h && !warnings?.length && !left) return null
  return (
    <>
      {h && (
        <p className={className} style={{ color: 'var(--text-dim)' }} role={h.empty ? undefined : 'status'}>
          {h.text}
          {h.link && (
            <>
              {' '}
              <Link to="/settings/indexers">{t('Settings > Indexers & Search')}</Link>.
            </>
          )}
        </p>
      )}
      {left && (
        <p className={className} style={{ color: 'var(--text-dim)' }} role="status">
          {left}
        </p>
      )}
      {warnings?.map((w) => (
        <p key={w} className={className} style={{ color: 'var(--text-dim)' }} role="status">
          {st(w)}
        </p>
      ))}
    </>
  )
}
