import { useCallback, useEffect, useState } from 'react'
import { api, type Settings as SettingsData } from '../../api'
import Icon from '../../components/Icon'
import Switch from '../../components/Switch'
import { useToast } from '../../components/Toast'
import { t } from '../../i18n'

const HARDWARE: { id: string; label: string; hint: string }[] = [
  { id: 'auto', label: t('Automatic'), hint: t('Uses a working hardware encoder when one is found, and software otherwise.') },
  { id: 'software', label: t('Software (libx264)'), hint: t('Works everywhere and is slower, especially for 4K.') },
  { id: 'vaapi', label: t('VAAPI (Intel and AMD on Linux)'), hint: t('Needs a GPU device mapped into the container, usually /dev/dri.') },
  { id: 'qsv', label: t('Intel Quick Sync'), hint: t('Needs a supported Intel GPU and its driver.') },
  { id: 'nvenc', label: t('NVIDIA NVENC'), hint: t('Needs an NVIDIA GPU and the NVIDIA container runtime.') },
]

// Settings > General > Playback: whether the built-in player may convert a
// video the browser cannot play, and which ffmpeg it uses.
export default function PlaybackSettings() {
  const toast = useToast()
  const [s, setS] = useState<SettingsData | null>(null)
  const [error, setError] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [ffmpegPath, setFfmpegPath] = useState('')
  const [hardware, setHardware] = useState('auto')
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [test, setTest] = useState('')

  const load = useCallback(() => {
    setError('')
    api
      .getSettings()
      .then((data) => {
        setS(data)
        setEnabled(data.transcodeEnabled !== false)
        setFfmpegPath(data.ffmpegPath ?? '')
        setHardware(data.transcodeHardware ?? 'auto')
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])
  useEffect(load, [load])

  async function save() {
    setSaving(true)
    try {
      const saved = await api.putSettings({ transcodeEnabled: enabled, ffmpegPath: ffmpegPath.trim(), transcodeHardware: hardware })
      setS(saved)
      setEnabled(saved.transcodeEnabled !== false)
      setFfmpegPath(saved.ffmpegPath ?? '')
      setHardware(saved.transcodeHardware ?? 'auto')
      setTest('')
      toast.success(t('Saved.'))
    } catch (e) {
      toast.error(t('Not saved: {error}', { error: e instanceof Error ? e.message : String(e) }))
    } finally {
      setSaving(false)
    }
  }

  async function check() {
    setTesting(true)
    setTest('')
    try {
      const r = await api.testFfmpeg(ffmpegPath.trim())
      const extra = [r.vaapi && 'VAAPI', r.qsv && 'QSV', r.nvenc && 'NVENC'].filter(Boolean).join(', ')
      setTest(extra ? t('{version} (hardware: {encoders})', { version: r.version, encoders: extra }) : r.version)
    } catch (e) {
      setTest(e instanceof Error ? e.message : String(e))
    } finally {
      setTesting(false)
    }
  }

  if (error) {
    return (
      <div>
        <p className="error-text">{error}</p>
        <button onClick={load}>{t('Try again')}</button>
      </div>
    )
  }
  if (!s) return <p>{t('Loading…')}</p>
  const chosen = HARDWARE.find((h) => h.id === hardware) ?? HARDWARE[0]

  return (
    <div className="settings-stack">
      <section className="card">
        <h2>
          <Icon name="film" size={16} /> {t('Videos the browser cannot play')}
        </h2>
        <Switch
          checked={enabled}
          onChange={setEnabled}
          disabled={saving}
          label={t('Convert videos that would otherwise have no picture or no sound')}
          description={t('The player asks the server about the file first. MP4 and WebM files the browser understands are sent as they are. Other containers, and files with audio codecs browsers do not support such as AC-3 or DTS, are converted to H.264 and AAC with ffmpeg and played as HLS. If ffmpeg is missing, the player falls back to sending the file directly, as before.')}
          showState
        />
      </section>

      <section className="card">
        <h2>
          <Icon name="hard" size={16} /> {t('ffmpeg')}
        </h2>
        <div className="settings-stack">
          <label>
            {t('ffmpeg path')}
            <input
              value={ffmpegPath}
              onChange={(e) => setFfmpegPath(e.target.value)}
              placeholder="ffmpeg"
              spellCheck={false}
              disabled={saving}
            />
            <small style={{ color: 'var(--text-dim)' }}>
              {t('Leave this empty to use the ffmpeg installed with Mediarium. To use your own build, enter the full path to its ffmpeg program; Mediarium looks for ffprobe next to it.')}
            </small>
          </label>
          <label>
            {t('Hardware acceleration')}
            <select value={hardware} onChange={(e) => setHardware(e.target.value)} disabled={saving}>
              {HARDWARE.map((h) => (
                <option key={h.id} value={h.id}>
                  {h.label}
                </option>
              ))}
            </select>
            <small style={{ color: 'var(--text-dim)' }}>{chosen.hint}</small>
          </label>
          <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 10 }}>
            <button className="primary" onClick={save} disabled={saving}>
              {t('Save')}
            </button>
            <button onClick={check} disabled={testing || saving}>
              {testing ? t('Checking…') : t('Test ffmpeg')}
            </button>
            <span className={test && !s.ffmpegAvailable ? 'error-text' : 'hint'}>{test || (s.ffmpegAvailable ? t('ffmpeg was found.') : t('ffmpeg was not found.'))}</span>
          </div>
        </div>
      </section>
    </div>
  )
}
