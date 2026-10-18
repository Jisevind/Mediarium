// Subtitles next to a video, turned into tracks the browser's own player can
// show. Browsers only read WebVTT, so SRT files are converted on the way.

import { t } from './i18n.ts'
import { languageName } from './languages.ts'

// srtToVtt converts the text of an SRT file to WebVTT: a WEBVTT header, and
// commas in the times become dots. A file that is already WebVTT is returned
// as it is.
export function srtToVtt(text: string): string {
  const body = text.replace(/^﻿/, '').replace(/\r\n?/g, '\n')
  if (/^WEBVTT/.test(body)) return body
  const fixed = body.replace(/(\d{1,2}:\d{2}:\d{2}),(\d{1,3})/g, '$1.$2')
  return `WEBVTT\n\n${fixed.trim()}\n`
}

export interface SubtitleTrack {
  path: string
  code: string // "en", "pt-BR": what comes after the video's name
}

// subtitleTracksFor finds the subtitle files that belong to a video: same
// folder, same name up to the language, as Mediarium and most players save
// them ("Film (2024).en.srt", "Film (2024).pt-BR.hi.srt"). SRT and VTT only.
export function subtitleTracksFor(videoPath: string, files: { path: string; kind: string }[]): SubtitleTrack[] {
  const dot = videoPath.lastIndexOf('.')
  const base = (dot > videoPath.lastIndexOf('/') ? videoPath.slice(0, dot) : videoPath).toLowerCase()
  const out: SubtitleTrack[] = []
  for (const f of files) {
    if (f.kind !== 'subtitle') continue
    const lower = f.path.toLowerCase()
    if (!lower.startsWith(base + '.')) continue
    const m = /^\.([a-z]{2,3}(?:-[a-z0-9]{2,4})?)(?:\.[a-z0-9-]+)*\.(?:srt|vtt)$/i.exec(f.path.slice(base.length))
    if (m) out.push({ path: f.path, code: m[1] })
  }
  return out
}

export interface EmbeddedSubtitle {
  language?: string
  title?: string
  forced?: boolean
  sdh?: boolean
}

// embeddedSubtitleLabel names a track inside the video the way the player's
// subtitle menu should show it. A track's own title wins when it says
// something more useful than just "SDH" or "Forced".
export function embeddedSubtitleLabel(track: EmbeddedSubtitle, index: number): string {
  const title = (track.title ?? '').trim()
  if (title && !/^(sdh|forced)$/i.test(title)) return title
  const language = track.language ? languageName(track.language) : t('Subtitle {n}', { n: index + 1 })
  const flags: string[] = []
  if (track.forced || /^forced$/i.test(title)) flags.push(t('forced'))
  if (track.sdh || /sdh/i.test(title)) flags.push('SDH')
  return [language, ...flags].join(' ')
}
