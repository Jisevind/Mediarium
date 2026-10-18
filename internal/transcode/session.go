package transcode

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultSessionTTL   = 15 * time.Minute
	defaultMaxSessions  = 8
	defaultMaxEncodes   = 4
	segmentEncodeWindow = 3 * time.Minute
)

// Manager keeps the HLS sessions that are being watched and the temporary
// files they have made. Segments are encoded on demand, so an hour-long film
// only costs the parts somebody actually watched.
type Manager struct {
	root        string
	ttl         time.Duration
	maxSessions int
	sem         chan struct{}
	now         func() time.Time

	mu       sync.Mutex
	sessions map[string]*Session
}

// NewManager makes a manager whose sessions live in the system temporary
// folder. The container's /tmp goes away on a restart; native installs get a
// cleanup pass on the next start.
func NewManager() *Manager {
	return NewManagerAt(os.TempDir())
}

// NewManagerAt is NewManager with a chosen parent folder, for tests and for a
// future setting.
func NewManagerAt(root string) *Manager {
	_ = os.MkdirAll(root, 0o755)
	return &Manager{
		root: root, ttl: defaultSessionTTL, maxSessions: defaultMaxSessions,
		sem: make(chan struct{}, defaultMaxEncodes), now: time.Now, sessions: map[string]*Session{},
	}
}

// Session is one playback of one file.
type Session struct {
	ID      string
	UserID  int64
	Input   string
	Plan    Plan
	Encoder Encoder
	Dir     string
	Created time.Time

	mu       sync.Mutex
	segments []Segment
	states   map[int]*segmentState
	sem      chan struct{}
	busy     int
	closed   bool
	lastUsed time.Time
}

type segmentState struct {
	done chan struct{}
	path string
	err  error
}

// Create starts a session. keyframes come from the probe; without them the
// file is cut at fixed intervals.
func (m *Manager) Create(input string, plan Plan, duration float64, keyframes []float64, userID int64, enc Encoder) (*Session, error) {
	// A transcode can seek accurately, so fixed segments are better even when
	// the source's keyframes are far apart. A stream copy can only start on a
	// source keyframe, so its segments follow the keyframes instead.
	if !plan.VideoCopy {
		keyframes = nil
	}
	segments := Segments(keyframes, duration, 4)
	if len(segments) == 0 {
		return nil, errors.New("the file has no playable length")
	}
	m.mu.Lock()
	m.cleanupLocked()
	for len(m.sessions) >= m.maxSessions {
		oldest := m.oldestIdleLocked()
		if oldest == "" {
			m.mu.Unlock()
			return nil, errors.New("too many videos are being transcoded at once")
		}
		m.removeLocked(oldest)
	}
	dir, err := os.MkdirTemp(m.root, "mediarium-hls-*")
	if err != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("make a transcode folder: %w", err)
	}
	id := randomID()
	now := m.now()
	s := &Session{
		ID: id, UserID: userID, Input: input, Plan: plan, Encoder: enc, Dir: dir, Created: now, lastUsed: now,
		segments: segments, states: map[int]*segmentState{}, sem: m.sem,
	}
	m.sessions[id] = s
	m.mu.Unlock()
	return s, nil
}

// Get returns a live session.
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

// Close stops a session and removes its temporary files.
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if !ok {
		return nil
	}
	s.mu.Lock()
	s.closed = true
	remove := s.busy == 0
	dir := s.Dir
	s.mu.Unlock()
	if remove {
		return os.RemoveAll(dir)
	}
	return nil
}

// Cleanup drops sessions nobody has asked for in a while. It is also run when
// a new session starts, so a long-running server does not need a goroutine.
func (m *Manager) Cleanup() {
	m.mu.Lock()
	m.cleanupLocked()
	m.mu.Unlock()
}

func (m *Manager) cleanupLocked() {
	for id, s := range m.sessions {
		s.mu.Lock()
		idle := m.now().Sub(s.lastUsed) > m.ttl && s.busy == 0
		s.mu.Unlock()
		if idle {
			m.removeLocked(id)
		}
	}
}

func (m *Manager) removeLocked(id string) {
	s, ok := m.sessions[id]
	if !ok {
		return
	}
	delete(m.sessions, id)
	s.mu.Lock()
	s.closed = true
	idle := s.busy == 0
	dir := s.Dir
	s.mu.Unlock()
	if idle {
		_ = os.RemoveAll(dir)
	}
}

func (m *Manager) oldestIdleLocked() string {
	var oldest string
	var when time.Time
	for id, s := range m.sessions {
		s.mu.Lock()
		idle := s.busy == 0
		used := s.lastUsed
		s.mu.Unlock()
		if idle && (oldest == "" || used.Before(when)) {
			oldest, when = id, used
		}
	}
	return oldest
}

// Manifest is the session's complete VOD playlist.
func (s *Session) Manifest() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Playlist(s.segments)
}

// Segments returns the session's segment list.
func (s *Session) Segments() []Segment {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Segment(nil), s.segments...)
}

// Directory is where the session's segments are kept.
func (m *Manager) Directory(s *Session) string { return s.Dir }

// EnsureSegment makes one segment and returns its file. Asking for the same
// segment twice encodes it once; a segment that was already made is reused.
func (s *Session) EnsureSegment(ctx context.Context, index int, ffmpegPath string) (string, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return "", errors.New("the playback session has ended")
	}
	var seg *Segment
	for i := range s.segments {
		if s.segments[i].Index == index {
			seg = &s.segments[i]
			break
		}
	}
	if seg == nil {
		s.mu.Unlock()
		return "", errors.New("no such segment")
	}
	path := filepath.Join(s.Dir, fmt.Sprintf("seg-%d.ts", index))
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		s.lastUsed = time.Now()
		s.mu.Unlock()
		return path, nil
	}
	if st, ok := s.states[index]; ok {
		s.mu.Unlock()
		select {
		case <-st.done:
			return st.path, st.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	st := &segmentState{done: make(chan struct{})}
	s.states[index] = st
	s.busy++
	s.lastUsed = time.Now()
	s.mu.Unlock()

	err := s.encode(ctx, *seg, path, ffmpegPath)
	s.mu.Lock()
	st.path, st.err = path, err
	close(st.done)
	delete(s.states, index)
	s.busy--
	s.lastUsed = time.Now()
	remove := s.closed && s.busy == 0
	dir := s.Dir
	s.mu.Unlock()
	if remove {
		_ = os.RemoveAll(dir)
	}
	if err != nil {
		return "", err
	}
	return path, nil
}

func (s *Session) encode(ctx context.Context, seg Segment, output, ffmpegPath string) error {
	select {
	case s.semaphore() <- struct{}{}:
		defer func() { <-s.semaphore() }()
	case <-ctx.Done():
		return ctx.Err()
	}
	tmp := output + ".tmp"
	_ = os.Remove(tmp)
	encodeCtx, cancel := context.WithTimeout(context.Background(), segmentEncodeWindow)
	defer cancel()
	cmd := exec.CommandContext(encodeCtx, ffmpegPath, SegmentArgs(s.Input, seg, s.Plan, s.Encoder, tmp)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(tmp)
		detail := strings.TrimSpace(string(out))
		if len(detail) > 500 {
			detail = detail[len(detail)-500:]
		}
		return fmt.Errorf("encode segment %d: %w: %s", seg.Index, err, detail)
	}
	if info, err := os.Stat(tmp); err != nil || info.Size() == 0 {
		_ = os.Remove(tmp)
		if err == nil {
			err = errors.New("ffmpeg wrote an empty segment")
		}
		return fmt.Errorf("save segment %d: %w", seg.Index, err)
	}
	if err := os.Rename(tmp, output); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("save segment %d: %w", seg.Index, err)
	}
	return nil
}

// semaphore is filled in by Manager.Create; a Session cannot know its manager
// otherwise. It is a method so Session stays usable in tests.
func (s *Session) semaphore() chan struct{} { return s.sem }

func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand does not fail on the systems Mediarium runs on; a
		// timestamp still keeps the ID unguessable enough to be useful.
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
