package transcode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Prober asks ffprobe what is inside a file and remembers the answer while
// the file's size and modification time stay the same.
type Prober struct {
	mu     sync.Mutex
	media  map[string]Media
	frames map[string][]float64
}

// NewProber returns an empty probe cache.
func NewProber() *Prober {
	return &Prober{media: map[string]Media{}, frames: map[string][]float64{}}
}

// FFprobePath finds ffprobe next to the configured ffmpeg. An empty setting
// means both commands are on PATH.
func FFprobePath(ffmpegPath string) string {
	if ffmpegPath == "" {
		return "ffprobe"
	}
	base := filepath.Base(ffmpegPath)
	if strings.HasPrefix(base, "ffmpeg") {
		return filepath.Join(filepath.Dir(ffmpegPath), "ffprobe"+strings.TrimPrefix(base, "ffmpeg"))
	}
	return "ffprobe"
}

// Probe reads a file's container, duration and streams.
func (p *Prober) Probe(ctx context.Context, ffmpegPath, path string) (Media, error) {
	key, ok := cacheKey(path)
	if !ok {
		return Media{}, fmt.Errorf("stat %s: %w", filepath.Base(path), os.ErrNotExist)
	}
	p.mu.Lock()
	if m, hit := p.media[key]; hit {
		p.mu.Unlock()
		return m, nil
	}
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, FFprobePath(ffmpegPath), "-v", "error", "-print_format", "json", "-show_format", "-show_streams", path).Output()
	if err != nil {
		return Media{}, fmt.Errorf("probe %s: %w", filepath.Base(path), err)
	}
	m, err := parseProbe(out, path)
	if err != nil {
		return Media{}, err
	}
	p.mu.Lock()
	if len(p.media) >= 1000 {
		for k := range p.media {
			delete(p.media, k)
			break
		}
	}
	p.media[key] = m
	p.mu.Unlock()
	return m, nil
}

// Keyframes lists the video's keyframe times. An unreadable file has none,
// which makes Segments fall back to fixed boundaries.
func (p *Prober) Keyframes(ctx context.Context, ffmpegPath, path string) []float64 {
	key, ok := cacheKey(path)
	if !ok {
		return nil
	}
	p.mu.Lock()
	if f, hit := p.frames[key]; hit {
		p.mu.Unlock()
		return f
	}
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, FFprobePath(ffmpegPath), "-v", "error", "-select_streams", "v:0", "-skip_frame", "nokey", "-show_entries", "frame=pts_time", "-of", "csv=p=0", path).Output()
	if err != nil {
		return nil
	}
	frames := parseKeyframes(out)
	p.mu.Lock()
	if len(p.frames) >= 1000 {
		for k := range p.frames {
			delete(p.frames, k)
			break
		}
	}
	p.frames[key] = frames
	p.mu.Unlock()
	return frames
}

// cacheKey is a file's identity for the probe cache.
func cacheKey(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	return fmt.Sprintf("%s|%d|%d", path, info.Size(), info.ModTime().UnixNano()), true
}

type probeJSON struct {
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
	} `json:"format"`
	Streams []struct {
		Index       int    `json:"index"`
		CodecType   string `json:"codec_type"`
		CodecName   string `json:"codec_name"`
		Profile     string `json:"profile"`
		PixFmt      string `json:"pix_fmt"`
		Width       int    `json:"width"`
		Height      int    `json:"height"`
		Channels    int    `json:"channels"`
		Disposition struct {
			Default     int `json:"default"`
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
	} `json:"streams"`
}

func parseProbe(data []byte, name string) (Media, error) {
	var p probeJSON
	if err := json.Unmarshal(data, &p); err != nil {
		return Media{}, fmt.Errorf("read ffprobe answer: %w", err)
	}
	duration, err := strconv.ParseFloat(p.Format.Duration, 64)
	if err != nil || duration <= 0 {
		return Media{}, fmt.Errorf("ffprobe found no duration for %s", filepath.Base(name))
	}
	m := Media{Container: ClassifyContainer(p.Format.FormatName, name), Duration: duration}
	audioOrdinal := 0
	for _, s := range p.Streams {
		switch s.CodecType {
		case "video":
			if s.Disposition.AttachedPic == 1 {
				continue // cover art inside the file, not the film
			}
			if m.Video == nil {
				m.Video = &Video{
					Codec: strings.ToLower(s.CodecName), Profile: s.Profile, PixFmt: s.PixFmt,
					Width: s.Width, Height: s.Height,
				}
			}
		case "audio":
			a := &Audio{Codec: strings.ToLower(s.CodecName), Channels: s.Channels, Default: s.Disposition.Default == 1, Ordinal: audioOrdinal}
			audioOrdinal++
			if m.Audio == nil || a.Default {
				m.Audio = a
			}
		}
	}
	return m, nil
}

func parseKeyframes(data []byte) []float64 {
	var out []float64
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if comma := strings.IndexByte(line, ','); comma >= 0 {
			line = line[:comma]
		}
		if t, err := strconv.ParseFloat(strings.TrimSpace(line), 64); err == nil && t >= 0 {
			out = append(out, t)
		}
	}
	return out
}
