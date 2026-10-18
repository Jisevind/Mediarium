// Package transcode decides whether a video file can be played as it is, and
// turns the files that cannot into short HLS segments with ffmpeg. The
// decision is based on the streams inside the file, not only its extension:
// a perfectly good MP4 with AC-3 audio still has no sound in most browsers.
package transcode

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Container is the container family a file belongs to, as far as playback in
// a browser is concerned.
type Container string

const (
	ContainerMP4      Container = "mp4"
	ContainerWebM     Container = "webm"
	ContainerMatroska Container = "matroska"
	ContainerOther    Container = "other"
)

// ClassifyContainer names the container from ffprobe's format name and the
// file name. The extension wins where a format name covers two containers
// ("matroska,webm"): a .webm is WebM, a .mkv is Matroska.
func ClassifyContainer(format, name string) Container {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".m4v", ".mov":
		return ContainerMP4
	case ".webm":
		return ContainerWebM
	case ".mkv", ".mk3d":
		return ContainerMatroska
	}
	f := strings.ToLower(format)
	switch {
	case strings.Contains(f, "matroska"):
		return ContainerMatroska
	case strings.Contains(f, "webm"):
		return ContainerWebM
	case strings.Contains(f, "mp4"), strings.Contains(f, "mov"):
		return ContainerMP4
	}
	return ContainerOther
}

// Mode is what a file needs before the browser can play it.
type Mode string

const (
	// ModeDirect sends the file as it is.
	ModeDirect Mode = "direct"
	// ModeRemux copies the video into HLS and makes the audio AAC.
	ModeRemux Mode = "remux"
	// ModeTranscode encodes the video to H.264 as well.
	ModeTranscode Mode = "transcode"
)

// Video is the video stream that matters for playback.
type Video struct {
	Codec   string
	Profile string
	PixFmt  string
	Width   int
	Height  int
}

// Audio is the audio stream that will be played.
type Audio struct {
	Codec    string
	Channels int
	Default  bool
	Ordinal  int // 0 for the first audio stream, 1 for the second, and so on
}

// Media is what a probe says about a file.
type Media struct {
	Container Container
	Duration  float64
	Video     *Video
	Audio     *Audio
}

// Plan is how a file will be played.
type Plan struct {
	Mode       Mode
	VideoCopy  bool
	AudioIndex int    // which audio stream to map, relative to the audio streams
	Reason     string // why it isn't a direct play, in a few words
}

// PlanFor decides between direct play, an HLS remux and a full transcode.
// The direct-play rules are deliberately conservative: an unnecessary
// transcode costs CPU, but a wrong direct play is the silent-video bug this
// exists to fix.
func PlanFor(m Media) Plan {
	if m.Video == nil {
		return Plan{Mode: ModeDirect}
	}
	audioIndex := 0
	if m.Audio != nil {
		audioIndex = m.Audio.Ordinal
	}
	audioSafe := func(codecs ...string) bool {
		if m.Audio == nil {
			return true
		}
		for _, c := range codecs {
			if strings.EqualFold(m.Audio.Codec, c) {
				return true
			}
		}
		return false
	}
	switch m.Container {
	case ContainerMP4:
		if h264EightBit(m.Video) && audioSafe("aac", "mp3") {
			return Plan{Mode: ModeDirect, AudioIndex: audioIndex}
		}
	case ContainerWebM:
		if webmVideo(m.Video) && audioSafe("opus", "vorbis") {
			return Plan{Mode: ModeDirect, AudioIndex: audioIndex}
		}
	}
	// An MP4 with H.264 video can keep its video stream when only the audio
	// or the container is the problem. Matroska is deliberately not copied:
	// seeking with -ss in Matroska starts at the previous keyframe while the
	// audio starts at the requested time, which makes the sound drift from the
	// picture. Encoding the video is slower but keeps them together.
	if h264EightBit(m.Video) && m.Container == ContainerMP4 {
		reason := "the audio needs converting"
		if m.Audio != nil && !audioSafe("aac", "mp3", "opus", "vorbis") {
			reason = fmt.Sprintf("the %s audio isn't supported", strings.ToUpper(m.Audio.Codec))
		}
		return Plan{Mode: ModeRemux, VideoCopy: true, AudioIndex: audioIndex, Reason: reason}
	}
	reason := fmt.Sprintf("%s video needs converting", strings.ToUpper(m.Video.Codec))
	if strings.EqualFold(m.Video.Codec, "h264") {
		if m.Container == ContainerMatroska {
			reason = "the MKV container needs converting"
		} else {
			reason = "10-bit or unsupported H.264 needs converting"
		}
	}
	return Plan{Mode: ModeTranscode, AudioIndex: audioIndex, Reason: reason}
}

func h264EightBit(v *Video) bool {
	if !strings.EqualFold(v.Codec, "h264") {
		return false
	}
	p := strings.ToLower(v.Profile)
	for _, bad := range []string{"10", "422", "4:2:2", "444", "4:4:4"} {
		if strings.Contains(p, bad) {
			return false
		}
	}
	pf := strings.ToLower(v.PixFmt)
	switch pf {
	case "", "yuv420p", "yuvj420p", "nv12", "nv21":
		return true
	}
	return false
}

func webmVideo(v *Video) bool {
	switch strings.ToLower(v.Codec) {
	case "vp8", "vp9", "av1":
		return true
	}
	return false
}
