package api

import (
	"context"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/transcode"
)

// transcodeEnabled is the master switch for on-demand transcoding. On unless
// explicitly turned off, so the files that need it play out of the box.
func (s *Server) transcodeEnabled() bool {
	v, _ := s.Settings.Get(settings.KeyTranscodeEnabled)
	return v != "0"
}

// ffmpegPath is the ffmpeg command the transcoder runs. An empty setting means
// the ffmpeg on PATH, which is the one the container installs.
func (s *Server) ffmpegPath() string {
	v, _ := s.Settings.Get(settings.KeyTranscodeFFmpeg)
	if v = strings.TrimSpace(v); v != "" {
		return v
	}
	return "ffmpeg"
}

// transcodeHardware is the encoder preference: auto, software, vaapi, qsv or
// nvenc.
func (s *Server) transcodeHardware() string {
	v, _ := s.Settings.Get(settings.KeyTranscodeHardware)
	if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
		return v
	}
	return "auto"
}

// ffmpegFound reports whether the configured command can be run. It is shown
// on Settings > Playback so a missing binary is visible before playback.
func (s *Server) ffmpegFound() bool {
	ffmpeg := s.ffmpegPath()
	if _, err := exec.LookPath(ffmpeg); err != nil {
		return false
	}
	_, err := exec.LookPath(transcode.FFprobePath(ffmpeg))
	return err == nil
}

// chooseEncoder picks the encoder for a new playback. A hardware choice that
// cannot run falls back to libx264 here rather than failing the film.
func (s *Server) chooseEncoder(ctx context.Context) transcode.Encoder {
	ffmpeg, hardware := s.ffmpegPath(), s.transcodeHardware()
	key := ffmpeg + "|" + hardware
	s.encoderMu.Lock()
	if s.encoderKey == key {
		enc := s.encoderCache
		s.encoderMu.Unlock()
		return enc
	}
	s.encoderMu.Unlock()
	enc, err := transcode.ChooseEncoder(ctx, ffmpeg, hardware, "")
	if err != nil {
		slog.Warn("transcode: the chosen hardware encoder isn't usable, using software", "err", err)
		enc = transcode.SoftwareEncoder()
	}
	s.encoderMu.Lock()
	s.encoderKey, s.encoderCache = key, enc
	s.encoderMu.Unlock()
	return enc
}

// handleTestFFmpeg runs the configured ffmpeg and reports what it is. ffmpeg
// can be present but unusable (wrong architecture, missing libraries), so the
// Settings page can ask rather than guess.
func (s *Server) handleTestFFmpeg(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	_ = decodeJSON(r, &req) // an empty body tests the saved path
	path := strings.TrimSpace(req.Path)
	if path == "" {
		path = s.ffmpegPath()
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "-hide_banner", "-version").CombinedOutput()
	if err != nil {
		writeError(w, http.StatusBadRequest, "ffmpeg couldn't be run. Check the path, or leave it empty to use the ffmpeg installed with Mediarium.")
		return
	}
	line := strings.TrimSpace(string(out))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	encoders, _ := transcode.AvailableEncoders(ctx, path)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "version": line,
		"vaapi": encoders["h264_vaapi"], "qsv": encoders["h264_qsv"], "nvenc": encoders["h264_nvenc"],
	})
}
