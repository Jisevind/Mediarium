package api

import (
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/mediafiles"
	"github.com/rdborg/mediarium/internal/subtitles"
	"github.com/rdborg/mediarium/internal/transcode"
)

// playPayload is the answer to "how should this file be played?". Direct play
// points at the existing stream endpoint, untouched. HLS points at a scoped
// playlist whose segments are made as they are watched.
type playPayload struct {
	Mode      string            `json:"mode"` // direct | hls
	URL       string            `json:"url"`
	Reason    string            `json:"reason,omitempty"`
	Duration  float64           `json:"duration,omitempty"`
	Subtitles []subtitlePayload `json:"subtitles,omitempty"`
}

// subtitlePayload is one text track inside the video, ready for a <track>.
type subtitlePayload struct {
	URL      string `json:"url"`
	Language string `json:"language,omitempty"`
	Title    string `json:"title,omitempty"`
	Forced   bool   `json:"forced,omitempty"`
	SDH      bool   `json:"sdh,omitempty"`
}

// handlePlayFile decides whether a video can be played as it is or needs
// HLS. It probes the streams, not just the extension: an MP4 with AC-3 audio
// would play with no sound otherwise. A missing or broken ffmpeg falls back to
// direct play, which is exactly what the player did before transcoding.
func (s *Server) handlePlayFile(w http.ResponseWriter, r *http.Request) {
	target, ok := s.resolveStreamTarget(w, r)
	if !ok {
		return
	}
	input, ok := s.resolvePlayFile(w, target)
	if !ok {
		return
	}
	subs := s.embeddedSubtitlePayloads(r, target, input)
	direct := func(reason string) {
		w.Header().Set("Cache-Control", "private, no-store")
		writeJSON(w, http.StatusOK, playPayload{Mode: "direct", URL: "/api/files/stream?" + r.URL.RawQuery, Reason: reason, Subtitles: subs})
	}
	// Album tracks and pictures keep the direct path; the handshake exists for
	// video files.
	if target.album || mediafiles.VideoContentType(target.rel) == "" {
		direct("")
		return
	}
	if !s.transcodeEnabled() {
		direct("transcoding is switched off")
		return
	}
	if !s.ffmpegFound() {
		direct("ffmpeg isn't available")
		return
	}

	media, err := s.prober.Probe(r.Context(), s.ffmpegPath(), input)
	if err != nil {
		slog.Warn("transcode: couldn't probe a video, playing it directly", "file", path.Base(target.rel), "err", err)
		direct("the video couldn't be checked")
		return
	}
	if media.Video == nil {
		direct("")
		return
	}
	plan := transcode.PlanFor(media)
	if plan.Mode == transcode.ModeDirect {
		direct("")
		return
	}

	user := auth.UserFromContext(r.Context())
	var userID int64
	if user != nil {
		userID = user.ID
	}
	// The keyframe scan and the encoder test do not need each other. Running
	// them together keeps the play handshake inside the browser's read timeout.
	var keyframes []float64
	enc := transcode.SoftwareEncoder() // unused by a stream copy
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); keyframes = s.prober.Keyframes(r.Context(), s.ffmpegPath(), input) }()
	if !plan.VideoCopy {
		wg.Add(1)
		go func() { defer wg.Done(); enc = s.chooseEncoder(r.Context()) }()
	}
	wg.Wait()
	if plan.VideoCopy && len(keyframes) < 2 {
		// Without usable keyframes a stream copy cannot be cut at a seek
		// point. Encode the video instead of returning a segment that starts
		// on the wrong picture.
		plan = transcode.Plan{Mode: transcode.ModeTranscode, AudioIndex: plan.AudioIndex, Reason: "the video needs converting for reliable seeking"}
		enc = s.chooseEncoder(r.Context())
	}
	session, err := s.transcoder.Create(input, plan, media.Duration, keyframes, userID, enc)
	if err != nil {
		slog.Warn("transcode: couldn't start a session", "file", path.Base(target.rel), "err", err)
		writeError(w, http.StatusServiceUnavailable, "Couldn't start converting this video. Try again in a moment.")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, playPayload{
		Mode: "hls", URL: "/api/files/hls/" + session.ID + "/index.m3u8",
		Reason: plan.Reason, Duration: media.Duration, Subtitles: subs,
	})
}

// resolvePlayFile turns a stream target into the absolute file path ffmpeg
// and the subtitle reader use, answering the usual path errors itself.
func (s *Server) resolvePlayFile(w http.ResponseWriter, target streamTarget) (string, bool) {
	input, err := target.folder.Path(target.rel)
	switch {
	case errors.Is(err, mediafiles.ErrInvalidPath):
		writeError(w, http.StatusBadRequest, "That file path isn't valid.")
		return "", false
	case errors.Is(err, fs.ErrNotExist):
		writeError(w, http.StatusNotFound, "Couldn't find that file. It may have been moved or deleted.")
		return "", false
	case err != nil:
		writeFileReadError(w, err)
		return "", false
	}
	return input, true
}

// embeddedSubtitlePayloads lists the text subtitle tracks inside a video.
// Bitmap tracks are left out: a browser cannot show them as WebVTT.
func (s *Server) embeddedSubtitlePayloads(r *http.Request, target streamTarget, input string) []subtitlePayload {
	if target.album || mediafiles.VideoContentType(target.rel) == "" || !s.ffmpegFound() {
		return nil
	}
	tracks, err := subtitles.EmbeddedSubtitles(input)
	if err != nil {
		return nil
	}
	base := url.Values{}
	for _, k := range []string{"movie", "series", "album"} {
		if v := r.URL.Query().Get(k); v != "" {
			base.Set(k, v)
		}
	}
	base.Set("path", target.rel)
	out := make([]subtitlePayload, 0, len(tracks))
	for i, t := range tracks {
		if !subtitles.IsTextSubtitle(t.Codec) {
			continue
		}
		q := make(url.Values, len(base)+1)
		for k, vs := range base {
			q[k] = append([]string(nil), vs...)
		}
		q.Set("stream", strconv.Itoa(i))
		out = append(out, subtitlePayload{
			URL: "/api/files/subtitle?" + q.Encode(), Language: t.Lang, Title: t.Name,
			Forced: t.Forced, SDH: t.SDH,
		})
	}
	return out
}

// handleSubtitleFile turns one embedded text subtitle into WebVTT for the
// player's <track>. It is separate from playback because the browser loads
// the track by itself, both for direct play and for HLS.
func (s *Server) handleSubtitleFile(w http.ResponseWriter, r *http.Request) {
	target, ok := s.resolveStreamTarget(w, r)
	if !ok {
		return
	}
	if target.album || mediafiles.VideoContentType(target.rel) == "" {
		writeError(w, http.StatusNotFound, "That subtitle track isn't a text track Mediarium can show.")
		return
	}
	stream, err := strconv.Atoi(r.URL.Query().Get("stream"))
	if err != nil || stream < 0 {
		writeError(w, http.StatusBadRequest, "That isn't a valid subtitle track.")
		return
	}
	input, ok := s.resolvePlayFile(w, target)
	if !ok {
		return
	}
	tracks, err := subtitles.EmbeddedSubtitles(input)
	if err != nil || stream >= len(tracks) || !subtitles.IsTextSubtitle(tracks[stream].Codec) {
		writeError(w, http.StatusNotFound, "That subtitle track isn't a text track Mediarium can show.")
		return
	}
	data, err := s.prober.SubtitleVTT(r.Context(), s.ffmpegPath(), input, stream)
	if err != nil {
		slog.Warn("subtitle: couldn't extract a track", "file", path.Base(target.rel), "stream", stream, "err", err)
		writeError(w, http.StatusBadGateway, "Couldn't read this subtitle. Check the server log for details.")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/vtt; charset=utf-8")
	h.Set("Cache-Control", "private, no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("Content-Disposition", "inline")
	_, _ = w.Write(data)
}

// sessionFor finds a session owned by the signed-in account. A session ID is
// random and short-lived; checking the owner stops one person's playlist from
// being used by another.
func (s *Server) sessionFor(w http.ResponseWriter, r *http.Request) *transcode.Session {
	id := r.PathValue("session")
	session, ok := s.transcoder.Get(id)
	user := auth.UserFromContext(r.Context())
	if !ok || user == nil || session.UserID != user.ID {
		writeError(w, http.StatusNotFound, "This playback has ended. Start it again.")
		return nil
	}
	return session
}

// handleHLSManifest serves one session's complete VOD playlist.
func (s *Server) handleHLSManifest(w http.ResponseWriter, r *http.Request) {
	session := s.sessionFor(w, r)
	if session == nil {
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/vnd.apple.mpegurl")
	h.Set("Cache-Control", "private, no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
	_, _ = w.Write([]byte(session.Manifest()))
}

// handleHLSSegment makes (or reuses) one segment and sends it. hls.js and
// native HLS both ask for the segments they need, so seeking only encodes the
// part that is watched.
func (s *Server) handleHLSSegment(w http.ResponseWriter, r *http.Request) {
	session := s.sessionFor(w, r)
	if session == nil {
		return
	}
	index, ok := segmentIndex(r.PathValue("segment"))
	if !ok {
		writeError(w, http.StatusNotFound, "This playback has ended. Start it again.")
		return
	}
	file, err := session.EnsureSegment(r.Context(), index, s.ffmpegPath())
	if err != nil {
		slog.Warn("transcode: couldn't make a segment", "session", session.ID, "segment", index, "err", err)
		writeError(w, http.StatusBadGateway, "Couldn't convert this part of the video. Check the server log for details.")
		return
	}
	f, err := os.Open(file)
	if err != nil {
		slog.Warn("transcode: couldn't open a made segment", "session", session.ID, "segment", index, "err", err)
		writeError(w, http.StatusBadGateway, "Couldn't read this part of the video. Try again.")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusBadGateway, "Couldn't read this part of the video. Try again.")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "video/mp2t")
	h.Set("Cache-Control", "private, max-age=3600")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("Content-Disposition", "inline")
	http.ServeContent(w, r, "segment.ts", info.ModTime(), f)
}

// handleCloseHLS stops a session when the player closes, so its temporary
// segments go away at once.
func (s *Server) handleCloseHLS(w http.ResponseWriter, r *http.Request) {
	session := s.sessionFor(w, r)
	if session == nil {
		return
	}
	if err := s.transcoder.Close(session.ID); err != nil {
		slog.Warn("transcode: couldn't remove a session", "session", session.ID, "err", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

func segmentIndex(name string) (int, bool) {
	if !strings.HasPrefix(name, "seg-") || !strings.HasSuffix(name, ".ts") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "seg-"), ".ts"))
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}
