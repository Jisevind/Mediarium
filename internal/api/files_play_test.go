package api_test

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/settings"
)

// fakeTools makes an ffprobe that answers from the file extension and an
// ffmpeg that writes a segment. Tests do not need real media files.
func fakeTools(t *testing.T, dir string) (ffmpeg, ffprobe string) {
	t.Helper()
	ffprobe = filepath.Join(dir, "ffprobe")
	probe := `#!/bin/sh
for last; do :; done
case " $* " in
  *" -skip_frame nokey "*)
    printf '0.000000\n4.000000\n8.000000\n'
    ;;
  *)
    case "$last" in
      *.mp4) cat <<'JSON'
{"format":{"format_name":"mov,mp4,m4a","duration":"12"},"streams":[
 {"index":0,"codec_type":"video","codec_name":"h264","profile":"High","pix_fmt":"yuv420p","width":1920,"height":1080},
 {"index":1,"codec_type":"audio","codec_name":"aac","channels":2,"disposition":{"default":1}}]}
JSON
      ;;
      *) cat <<'JSON'
{"format":{"format_name":"matroska,webm","duration":"12"},"streams":[
 {"index":0,"codec_type":"video","codec_name":"h264","profile":"High","pix_fmt":"yuv420p","width":1920,"height":1080},
 {"index":1,"codec_type":"audio","codec_name":"ac3","channels":6,"disposition":{"default":1}}]}
JSON
      ;;
    esac
    ;;
esac
`
	if err := os.WriteFile(ffprobe, []byte(probe), 0o755); err != nil {
		t.Fatal(err)
	}
	ffmpeg = filepath.Join(dir, "ffmpeg")
	encode := `#!/bin/sh
for last; do :; done
case " $* " in
  *" -version "*) echo "ffmpeg version fake";;
  *" webvtt "*) printf 'WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nHello\n';;
  *" -encoders "*) echo " V....D libx264              H.264";;
  *) printf 'segment' > "$last";;
esac
`
	if err := os.WriteFile(ffmpeg, []byte(encode), 0o755); err != nil {
		t.Fatal(err)
	}
	return ffmpeg, ffprobe
}

type playEnv struct {
	server *api.Server
	base   string
	admin  *http.Client
	member *http.Client
	movie  library.Movie
	dir    string
}

func newPlayEnv(t *testing.T) *playEnv {
	t.Helper()
	server, base, admin, member, _ := familyServer(t)
	movies := t.TempDir()
	if err := server.Settings.Set(settings.KeyMoviesPath, movies, false); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(movies, "Silent Film (1999)")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	direct := filepath.Join(dir, "Direct.mp4")
	if err := os.WriteFile(direct, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Silent.mkv"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "Silent Film", Year: 1999})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "WEBDL-1080p", direct); err != nil {
		t.Fatal(err)
	}
	ffmpeg, _ := fakeTools(t, t.TempDir())
	for k, v := range map[string]string{
		settings.KeyTranscodeEnabled:  "1",
		settings.KeyTranscodeFFmpeg:   ffmpeg,
		settings.KeyTranscodeHardware: "software",
	} {
		if err := server.Settings.Set(k, v, false); err != nil {
			t.Fatal(err)
		}
	}
	return &playEnv{server: server, base: base, admin: admin, member: member, movie: m, dir: dir}
}

func (e *playEnv) play(t *testing.T, client *http.Client, name string) (int, map[string]any) {
	t.Helper()
	status, body := doStatus(t, client, http.MethodGet, e.base+"/api/files/play?movie="+strconv.FormatInt(e.movie.ID, 10)+"&path="+url.QueryEscape(name))
	return status, body
}

func TestPlayHandshakeDirectAndHLS(t *testing.T) {
	e := newPlayEnv(t)
	status, body := e.play(t, e.member, "Direct.mp4")
	if status != http.StatusOK || body["mode"] != "direct" || !strings.HasPrefix(body["url"].(string), "/api/files/stream?") {
		t.Fatalf("direct: %d %v", status, body)
	}
	status, body = e.play(t, e.member, "Silent.mkv")
	if status != http.StatusOK || body["mode"] != "hls" {
		t.Fatalf("hls: %d %v", status, body)
	}
	manifestURL := e.base + body["url"].(string)
	resp, err := e.member.Get(manifestURL)
	if err != nil {
		t.Fatal(err)
	}
	manifest, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/vnd.apple.mpegurl" ||
		!strings.Contains(string(manifest), "seg-0.ts") || !strings.Contains(string(manifest), "#EXT-X-ENDLIST") {
		t.Fatalf("manifest: %d %q %s", resp.StatusCode, resp.Header.Get("Content-Type"), manifest)
	}
	segmentURL := strings.TrimSuffix(manifestURL, "index.m3u8") + "seg-0.ts"
	resp, err = e.member.Get(segmentURL)
	if err != nil {
		t.Fatal(err)
	}
	segment, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(segment) != "segment" || resp.Header.Get("Content-Type") != "video/mp2t" {
		t.Fatalf("segment: %d %q %q", resp.StatusCode, segment, resp.Header.Get("Content-Type"))
	}

	// Another account cannot use the session.
	if status, _ := doStatus(t, e.admin, http.MethodGet, segmentURL); status != http.StatusNotFound {
		t.Fatalf("another account's segment: want 404, got %d", status)
	}
	sessionURL := strings.TrimSuffix(manifestURL, "/index.m3u8")
	if status, _ := doStatus(t, e.member, http.MethodDelete, sessionURL); status != http.StatusNoContent {
		t.Fatalf("close session: %d", status)
	}
	if status, _ := doStatus(t, e.member, http.MethodGet, manifestURL); status != http.StatusNotFound {
		t.Fatalf("closed session: want 404, got %d", status)
	}
}

func TestPlayFallsBackToDirectWhenTranscodingIsOffOrFFmpegIsMissing(t *testing.T) {
	e := newPlayEnv(t)
	if err := e.server.Settings.Set(settings.KeyTranscodeEnabled, "0", false); err != nil {
		t.Fatal(err)
	}
	if status, body := e.play(t, e.member, "Silent.mkv"); status != http.StatusOK || body["mode"] != "direct" || !strings.Contains(body["reason"].(string), "switched off") {
		t.Fatalf("transcoding off: %d %v", status, body)
	}
	if err := e.server.Settings.Set(settings.KeyTranscodeEnabled, "1", false); err != nil {
		t.Fatal(err)
	}
	if err := e.server.Settings.Set(settings.KeyTranscodeFFmpeg, filepath.Join(t.TempDir(), "missing"), false); err != nil {
		t.Fatal(err)
	}
	if status, body := e.play(t, e.member, "Silent.mkv"); status != http.StatusOK || body["mode"] != "direct" || !strings.Contains(body["reason"].(string), "ffmpeg") {
		t.Fatalf("ffmpeg missing: %d %v", status, body)
	}
}

func TestPlayRefusesAnEscapingPath(t *testing.T) {
	e := newPlayEnv(t)
	for _, path := range []string{"../../secret.mp4", "/etc/passwd", "not-there.mkv"} {
		status, _ := e.play(t, e.member, path)
		if status != http.StatusBadRequest && status != http.StatusNotFound {
			t.Errorf("%s: want 400 or 404, got %d", path, status)
		}
	}
}

func TestTranscodeSettings(t *testing.T) {
	e := newPlayEnv(t)
	status, body := doStatus(t, e.admin, http.MethodGet, e.base+"/api/settings")
	if status != http.StatusOK || body["transcodeEnabled"] != true || body["transcodeHardware"] != "software" {
		t.Fatalf("settings: %d %v", status, body)
	}
	if status, _ := doStatus(t, e.admin, http.MethodPost, e.base+"/api/settings/test-ffmpeg"); status != http.StatusOK {
		t.Fatalf("test ffmpeg: %d", status)
	}
	putJSONStatus(t, e.admin, e.base+"/api/settings", map[string]any{
		"transcodeEnabled": false, "ffmpegPath": "/opt/custom/ffmpeg", "transcodeHardware": "vaapi",
	}, http.StatusOK)
	_, body = doStatus(t, e.admin, http.MethodGet, e.base+"/api/settings")
	if body["transcodeEnabled"] != false || body["ffmpegPath"] != "/opt/custom/ffmpeg" || body["transcodeHardware"] != "vaapi" {
		t.Fatalf("saved settings: %v", body)
	}
	putJSONStatus(t, e.admin, e.base+"/api/settings", map[string]any{"transcodeHardware": "magic"}, http.StatusBadRequest)
}

func TestPlayListsAndServesEmbeddedTextSubtitles(t *testing.T) {
	e := newPlayEnv(t)
	embedded, err := os.ReadFile(filepath.Join("..", "subtitles", "testdata", "embedded.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.dir, "Embedded.mkv"), embedded, 0o644); err != nil {
		t.Fatal(err)
	}
	status, body := e.play(t, e.member, "Embedded.mkv")
	if status != http.StatusOK || body["mode"] != "hls" {
		t.Fatalf("play: %d %v", status, body)
	}
	tracks, ok := body["subtitles"].([]any)
	if !ok || len(tracks) != 4 {
		t.Fatalf("subtitles: %v", body["subtitles"])
	}
	first := tracks[0].(map[string]any)
	subURL, _ := first["url"].(string)
	if subURL == "" || !strings.Contains(subURL, "/api/files/subtitle?") || !strings.Contains(subURL, "stream=0") || first["language"] != "en" {
		t.Fatalf("first subtitle: %v", first)
	}
	status, hdr, sub := getRaw(t, e.member, e.base+subURL)
	if status != http.StatusOK || hdr.Get("Content-Type") != "text/vtt; charset=utf-8" || !strings.Contains(string(sub), "WEBVTT") {
		t.Fatalf("subtitle: %d %q %q", status, hdr.Get("Content-Type"), sub)
	}
	if status, _, _ := getRaw(t, e.member, e.base+strings.Replace(subURL, "stream=0", "stream=99", 1)); status != http.StatusNotFound {
		t.Fatalf("unknown track: %d", status)
	}
	if status, _, _ := getRaw(t, e.member, e.base+"/api/files/subtitle?movie="+strconv.FormatInt(e.movie.ID, 10)+"&path="+url.QueryEscape("../../secret.mkv")+"&stream=0"); status != http.StatusBadRequest {
		t.Fatalf("escaping path: %d", status)
	}
}
