import { useState } from 'react'
import { api, type TrashFormatChange } from '../api'
import { plural, t } from '../i18n'
import { signed } from '../trashScores'
import { useAutosaveSetting } from '../useAutosave'
import { useConfirm } from './ConfirmProvider'
import Switch from './Switch'
import { useToast } from './Toast'

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

// What one changed format is told as: "conditions updated", "TRaSH score 500 to 600".
function changeNotes(c: TrashFormatChange): string {
  const notes: string[] = []
  if (c.conditions) notes.push(t('conditions updated'))
  if (c.scoreChanged) {
    notes.push(t('TRaSH score {from} to {to}', { from: signed(c.scoreFrom), to: signed(c.scoreTo) }))
    if (c.profiles.length > 0) notes.push(t('changes in {profiles}', { profiles: c.profiles.join(', ') }))
  }
  return notes.join(', ')
}

// Keeps the custom formats that came from the TRaSH Guides up to date: a
// switch for the daily update, and a button that shows what would change
// before it changes it.
export default function TrashUpdateBar({ onUpdated }: { onUpdated: () => void }) {
  const toast = useToast()
  const confirm = useConfirm()
  const [busy, setBusy] = useState(false)
  const auto = useAutosaveSetting<boolean>(
    (s) => s.trashFormatsAuto ?? false,
    (v) => ({ trashFormatsAuto: v }),
    (saved) => (saved ? t('Saved: the formats from the TRaSH Guides are kept up to date.') : t("Saved: the formats from the TRaSH Guides are only updated when you press the button.")),
    false,
  )

  async function update() {
    setBusy(true)
    try {
      const preview = await api.trashUpdatePreview()
      if (preview.changes.length === 0) {
        toast.success(t('Everything from the TRaSH Guides is up to date.'))
        return
      }
      const body = (
        <>
          <p>{plural(preview.changes.length, '{n} format changed:', '{n} formats changed:')}</p>
          <ul>
            {preview.changes.map((c) => (
              <li key={c.id}>
                <strong>{c.name}</strong>: {changeNotes(c)}
              </li>
            ))}
          </ul>
          <p>{t("Scores you changed yourself stay as they are. Formats you pasted or made yourself aren't touched.")}</p>
        </>
      )
      if (!(await confirm({ title: t('Update from the TRaSH Guides?'), body, confirmLabel: t('Update') }))) return
      const done = await api.trashUpdate()
      toast.success(plural(done.changes.length, 'Updated {n} custom format from the TRaSH Guides.', 'Updated {n} custom formats from the TRaSH Guides.'))
      onUpdated()
    } catch (e) {
      toast.error(errText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="trash-update-bar">
      <Switch
        checked={auto.value}
        onChange={(v) => void auto.change(v)}
        disabled={!auto.loaded || auto.saving}
        label={t('Keep TRaSH formats up to date')}
        description={t('Once a day Mediarium reads the TRaSH Guides again and updates the formats that came from them, and the scores that are still the ones TRaSH gave. Formats you pasted or made yourself are never changed.')}
        showState
      />
      <button type="button" onClick={() => void update()} disabled={busy}>
        {busy ? t('Checking…') : t('Update from TRaSH')}
      </button>
    </div>
  )
}
