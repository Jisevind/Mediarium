package transcode

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestRealFFmpegMakesAPlayableSegment is skipped when the machine has no
// ffmpeg. It checks the part unit tests cannot: that the argument list really
// produces an MPEG-TS segment with H.264 video and AAC audio.
func TestRealFFmpegExtractsAnEmbeddedTextSubtitle(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	p := NewProber()
	data, err := p.SubtitleVTT(context.Background(), ffmpeg, filepath.Join("..", "subtitles", "testdata", "embedded.mkv"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(data)), "WEBVTT") {
		t.Fatalf("subtitle = %q, want WebVTT", data)
	}
	if !strings.Contains(string(data), "-->") {
		t.Fatalf("subtitle has no cue: %q", data)
	}
}

func TestRealFFmpegMakesAPlayableSegment(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is not installed")
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "input.mp4")
	makeFile := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=24",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000",
		"-t", "6", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-g", "24", "-keyint_min", "24", "-sc_threshold", "0",
		"-c:a", "ac3", "-shortest", input)
	if out, err := makeFile.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg cannot make a test file here: %v: %s", err, out)
	}
	ctx := context.Background()
	p := NewProber()
	media, err := p.Probe(ctx, ffmpeg, input)
	if err != nil {
		t.Fatal(err)
	}
	plan := PlanFor(media)
	if plan.Mode != ModeRemux || !plan.VideoCopy {
		t.Fatalf("plan = %+v, want a remux", plan)
	}
	frames := p.Keyframes(ctx, ffmpeg, input)
	m := NewManagerAt(t.TempDir())
	s, err := m.Create(input, plan, media.Duration, frames, 1, SoftwareEncoder())
	if err != nil {
		t.Fatal(err)
	}
	segment, err := s.EnsureSegment(ctx, 0, ffmpeg)
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "stream=codec_type,codec_name", "-of", "csv=p=0", segment).Output()
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, "h264") || !strings.Contains(got, "aac") {
		t.Fatalf("segment streams = %q, want h264 and aac", got)
	}
	// A later segment must keep its place on the timeline, or seeking lands
	// on the wrong picture.
	second, err := s.EnsureSegment(ctx, 1, ffmpeg)
	if err != nil {
		t.Fatal(err)
	}
	start, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=start_time", "-of", "csv=p=0", second).Output()
	if err != nil {
		t.Fatal(err)
	}
	at, err := strconv.ParseFloat(strings.TrimSpace(string(start)), 64)
	if err != nil {
		t.Fatalf("read second segment start %q: %v", start, err)
	}
	if at < 3.9 || at > 4.1 {
		t.Fatalf("second segment starts at %q, want about 4.0", start)
	}
}
