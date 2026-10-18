# Playback in the built-in player

The **Play** button on a movie's or show's files opens Mediarium's own player. It asks the server one question first: can this file be sent as it is, or does the browser need a converted copy?

## Direct play, or converted

Mediarium reads the streams inside the file with `ffprobe`, not just the file ending. The answer decides between two paths:

- **Direct play.** The file is sent unchanged through `GET /api/files/stream`, with range requests, so the browser's own player can seek. This is the same path Mediarium always used.
- **HLS.** The file is converted to H.264 video and AAC audio in short segments, and the browser plays the playlist with hls.js (Safari and iOS use their own HLS support). Playback starts after the first segment, and seeking converts the part you jump to instead of the whole film.

A file stays on the direct path when it is an MP4/MOV with 8-bit H.264 video and AAC or MP3 audio, or a WebM with VP8, VP9 or AV1 video and Opus or Vorbis audio. Everything else is converted. The most common reason for a conversion is not the video at all: an MKV, MP4 or other file with AC-3, EAC-3, DTS or TrueHD audio plays a picture in most browsers but has no sound. An MP4 with H.264 video keeps its video stream and only the audio is turned into AAC. An MKV is re-encoded as well: seeking inside Matroska with a copied video stream starts the picture and the sound at slightly different places, which makes them drift apart.

A missing or broken `ffmpeg`, or a file that cannot be read, falls back to direct play exactly as before. The player then shows its usual "your browser can't play this file" message if the browser still cannot play it.

## Hardware acceleration

By default (**Automatic**) Mediarium tries VAAPI, then Intel Quick Sync, then NVIDIA NVENC, and uses the first one that actually encodes a test frame. If none works it uses the software encoder (`libx264`), which works everywhere but is slower, especially for 4K.

Hardware acceleration needs the device and driver inside the container:

- **VAAPI or QSV (Intel or AMD):** map the render device, for example `--device=/dev/dri:/dev/dri`, and make sure the container user may use it. On a compose file:

  ```yaml
  devices:
    - /dev/dri:/dev/dri
  ```

  Some distributions also need the container user in the `video` or `render` group.

- **NVENC (NVIDIA):** install the NVIDIA container runtime and start the container with `--gpus all` or the equivalent `deploy.resources.reservations.devices` entry. The ffmpeg build must include `h264_nvenc`.

The **Test ffmpeg** button on Settings > General > Playback reports the ffmpeg version and which hardware encoders it can see. Seeing an encoder in that list is not the same as it working: Mediarium still tests it before using it and falls back to software if the test fails.

## Settings

Settings > General > Playback (administrators) has:

- **Convert videos that would otherwise have no picture or no sound** — on by default. Turning it off makes every file use direct play.
- **ffmpeg path** — empty uses the `ffmpeg` installed with Mediarium. To use your own build, enter the full path to its `ffmpeg`; `ffprobe` is looked for in the same folder.
- **Hardware acceleration** — Automatic, Software, VAAPI, Quick Sync or NVENC.

The settings are stored as `playback.transcode_enabled`, `playback.ffmpeg_path` and `playback.transcode_hardware` (see [reference/settings-keys.md](./reference/settings-keys.md)).

## Subtitles

Subtitle files next to the video (`.srt` and `.vtt`) appear in the player's subtitle menu. Text tracks stored **inside** an MKV or MP4 are offered too: SubRip, ASS/SSA, WebVTT and MP4 `tx3g`/`mov_text`. Mediarium extracts the chosen track with ffmpeg as WebVTT and the browser shows it over direct play or HLS. The menu uses the track's language, and says `SDH` or `forced` when the file marks it so.

Bitmap subtitles (PGS, VobSub and DVB) are not offered: a browser cannot display them as text, and turning them into text needs OCR or burning them into the picture. They are left out of the menu.

## When it does not work

- **The film plays but has no sound.** Check Settings > General > Playback: the switch must be on, and **Test ffmpeg** must find a working ffmpeg. If the file has an unusual audio codec, the conversion should still fix it; if it does not, the server log names the ffmpeg error.
- **It takes a long time to start.** Conversion is happening on the server. On a small NAS, software 4K conversion is slow; map a GPU for hardware acceleration, or let the file direct-play when the browser supports it.
- **It stops after a while.** Look under Settings > System > Logs and errors. A full temporary folder is the usual cause: segments are written to the system temporary folder (`/tmp` in the container) while they are watched, and are removed when the player closes or after a period without use.
- **Only one person can watch at once.** Mediarium limits how many conversions run at the same time so one film cannot use every CPU core. A second viewer may see "Couldn't start converting this video"; try again when the first has finished starting.

## What it does not do yet

- Bitmap subtitle tracks (PGS, VobSub and DVB) are not supported. Text tracks inside the video, and SRT/VTT files next to it, are.
- HDR video is converted without tone mapping, so colours can look washed out. Direct play keeps the original colour.
- There is no quality or resolution picker; the converter aims for a good 1080p-class H.264 picture and stereo AAC audio.
- The audio track the file marks as default is used. There is no menu for the other tracks yet.
- Music tracks are not converted. The player still sends audio files directly.
