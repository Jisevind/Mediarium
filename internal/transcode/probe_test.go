package transcode

import (
	"strings"
	"testing"
)

const sampleProbe = `{
  "format": {"format_name": "matroska,webm", "duration": "123.456"},
  "streams": [
    {"index": 0, "codec_type": "video", "codec_name": "h264", "profile": "High", "pix_fmt": "yuv420p", "width": 1920, "height": 1080},
    {"index": 1, "codec_type": "audio", "codec_name": "aac", "channels": 2, "disposition": {"default": 0}},
    {"index": 2, "codec_type": "audio", "codec_name": "ac3", "channels": 6, "disposition": {"default": 1}}
  ]
}`

func TestParseProbeChoosesTheDefaultAudioStream(t *testing.T) {
	m, err := parseProbe([]byte(sampleProbe), "Film.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if m.Container != ContainerMatroska || m.Duration != 123.456 {
		t.Fatalf("media = %+v", m)
	}
	if m.Video == nil || m.Video.Codec != "h264" || m.Video.PixFmt != "yuv420p" || m.Video.Width != 1920 {
		t.Fatalf("video = %+v", m.Video)
	}
	if m.Audio == nil || m.Audio.Codec != "ac3" || m.Audio.Channels != 6 || !m.Audio.Default || m.Audio.Ordinal != 1 {
		t.Fatalf("audio = %+v", m.Audio)
	}
}

func TestParseProbeRejectsAFileWithoutDuration(t *testing.T) {
	if _, err := parseProbe([]byte(`{"format":{"format_name":"mp4"},"streams":[]}`), "x.mp4"); err == nil {
		t.Fatal("a probe without a duration must fail")
	}
}

func TestParseProbeFallsBackToTheFirstAudioStream(t *testing.T) {
	m, err := parseProbe([]byte(`{"format":{"format_name":"mov,mp4","duration":"4"},"streams":[
		{"index":0,"codec_type":"video","codec_name":"h264","pix_fmt":"yuv420p"},
		{"index":1,"codec_type":"audio","codec_name":"aac","channels":2}
	]}`), "x.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if m.Audio == nil || m.Audio.Codec != "aac" {
		t.Fatalf("audio = %+v", m.Audio)
	}
}

func TestParseProbeSkipsCoverArt(t *testing.T) {
	m, err := parseProbe([]byte(`{"format":{"format_name":"matroska,webm","duration":"4"},"streams":[
		{"index":0,"codec_type":"video","codec_name":"mjpeg","disposition":{"attached_pic":1}},
		{"index":1,"codec_type":"video","codec_name":"h264","pix_fmt":"yuv420p"},
		{"index":2,"codec_type":"audio","codec_name":"aac","channels":2}
	]}`), "x.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if m.Video == nil || m.Video.Codec != "h264" {
		t.Fatalf("video = %+v", m.Video)
	}
}

func TestParseProbeFindsNoVideoForAnAudioFile(t *testing.T) {
	m, err := parseProbe([]byte(`{"format":{"format_name":"flac","duration":"4"},"streams":[
		{"index":0,"codec_type":"audio","codec_name":"flac","channels":2}
	]}`), "track.flac")
	if err != nil {
		t.Fatal(err)
	}
	if m.Video != nil || m.Audio == nil {
		t.Fatalf("media = %+v", m)
	}
}

func TestParseKeyframes(t *testing.T) {
	got := parseKeyframes([]byte("0.000000\n4.000000\n8.000000\n"))
	want := []float64{0, 4, 8}
	if len(got) != len(want) {
		t.Fatalf("keyframes = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("keyframes = %v", got)
		}
	}
	if got := parseKeyframes([]byte("N/A\n0.5\nbad\n")); len(got) != 1 || got[0] != 0.5 {
		t.Fatalf("keyframes with junk = %v", got)
	}
}

func TestFFprobePathFollowsTheFFmpegSetting(t *testing.T) {
	for _, tc := range []struct{ ffmpeg, want string }{
		{"", "ffprobe"},
		{"ffmpeg", "ffprobe"},
		{"/opt/ffmpeg/ffmpeg", "/opt/ffmpeg/ffprobe"},
		{"/usr/bin/ffmpeg", "/usr/bin/ffprobe"},
	} {
		if got := FFprobePath(tc.ffmpeg); got != tc.want {
			t.Errorf("FFprobePath(%q) = %q, want %q", tc.ffmpeg, got, tc.want)
		}
	}
}

func TestPlanForSampleMkv(t *testing.T) {
	m, err := parseProbe([]byte(sampleProbe), "Film.mkv")
	if err != nil {
		t.Fatal(err)
	}
	plan := PlanFor(m)
	if plan.Mode != ModeTranscode || plan.VideoCopy || plan.AudioIndex != 1 || !strings.Contains(plan.Reason, "MKV") {
		t.Fatalf("plan = %+v", plan)
	}
}
