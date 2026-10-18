package transcode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeFFmpeg(t *testing.T, dir string) (path, log string) {
	t.Helper()
	log = filepath.Join(dir, "calls.log")
	path = filepath.Join(dir, "ffmpeg")
	script := "#!/bin/sh\nfor last; do :; done\nprintf 'segment' > \"$last\"\necho x >> " + log + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, log
}

func callsIn(t *testing.T, log string) int64 {
	t.Helper()
	body, _ := os.ReadFile(log)
	return int64(strings.Count(string(body), "x"))
}

func TestManagerCreatesASessionAndEncodesASegmentOnce(t *testing.T) {
	root := t.TempDir()
	m := NewManagerAt(root)
	ffmpeg, callLog := fakeFFmpeg(t, root)
	s, err := m.Create("/movies/Film.mkv", Plan{Mode: ModeRemux, VideoCopy: true}, 10, []float64{0, 4, 8}, 7, SoftwareEncoder())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.Manifest(), "seg-0.ts") || !strings.Contains(s.Manifest(), "seg-2.ts") {
		t.Fatalf("manifest:\n%s", s.Manifest())
	}
	ctx := context.Background()
	segment, err := s.EnsureSegment(ctx, 1, ffmpeg)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(segment)
	if string(body) != "segment" {
		t.Fatalf("segment = %q", body)
	}
	if _, err := s.EnsureSegment(ctx, 1, ffmpeg); err != nil {
		t.Fatal(err)
	}
	if got := callsIn(t, callLog); got != 1 {
		t.Fatalf("the cached segment was encoded %d times", got)
	}
	if _, err := s.EnsureSegment(ctx, 99, ffmpeg); err == nil {
		t.Fatal("an unknown segment must fail")
	}
}

func TestManagerCloseRemovesTheSession(t *testing.T) {
	root := t.TempDir()
	m := NewManagerAt(root)
	s, err := m.Create("/movies/Film.mkv", Plan{Mode: ModeRemux}, 10, nil, 1, SoftwareEncoder())
	if err != nil {
		t.Fatal(err)
	}
	dir := m.Directory(s)
	if err := m.Close(s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the session folder should be gone, stat: %v", err)
	}
	if _, ok := m.Get(s.ID); ok {
		t.Fatal("the closed session must not be found")
	}
}

func TestManagerCleanupDropsIdleSessions(t *testing.T) {
	m := NewManagerAt(t.TempDir())
	s, err := m.Create("/movies/Film.mkv", Plan{Mode: ModeRemux}, 10, nil, 1, SoftwareEncoder())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	m.now = func() time.Time { return now.Add(2 * time.Hour) }
	m.Cleanup()
	if _, ok := m.Get(s.ID); ok {
		t.Fatal("an old session should be cleaned up")
	}
}

func TestManagerLimitsSimultaneousSessions(t *testing.T) {
	m := NewManagerAt(t.TempDir())
	m.maxSessions = 2
	first, _ := m.Create("/movies/a.mkv", Plan{Mode: ModeRemux}, 10, nil, 1, SoftwareEncoder())
	second, _ := m.Create("/movies/b.mkv", Plan{Mode: ModeRemux}, 10, nil, 1, SoftwareEncoder())
	third, err := m.Create("/movies/c.mkv", Plan{Mode: ModeRemux}, 10, nil, 1, SoftwareEncoder())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Get(first.ID); ok {
		t.Fatal("the oldest idle session should make room")
	}
	for _, s := range []*Session{second, third} {
		if _, ok := m.Get(s.ID); !ok {
			t.Fatalf("session %s should still exist", s.ID)
		}
	}
}

func TestEnsureSegmentRejectsAnEmptySegment(t *testing.T) {
	root := t.TempDir()
	m := NewManagerAt(root)
	ffmpeg := filepath.Join(root, "ffmpeg")
	if err := os.WriteFile(ffmpeg, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, _ := m.Create("/movies/Film.mkv", Plan{Mode: ModeRemux}, 10, nil, 1, SoftwareEncoder())
	if _, err := s.EnsureSegment(context.Background(), 0, ffmpeg); err == nil {
		t.Fatal("an empty ffmpeg output must fail")
	}
}

func TestEnsureSegmentStopsWhenTheSessionIsClosed(t *testing.T) {
	root := t.TempDir()
	m := NewManagerAt(root)
	ffmpeg, _ := fakeFFmpeg(t, root)
	s, _ := m.Create("/movies/Film.mkv", Plan{Mode: ModeRemux}, 10, nil, 1, SoftwareEncoder())
	if err := m.Close(s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureSegment(context.Background(), 0, ffmpeg); err == nil {
		t.Fatal("a closed session must not encode")
	}
}
