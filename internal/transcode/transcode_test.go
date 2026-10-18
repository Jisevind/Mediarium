package transcode

import "testing"

func TestPlanFor(t *testing.T) {
	tests := []struct {
		name string
		m    Media
		want Mode
		copy bool
	}{
		{
			name: "mp4 h264 aac direct",
			m:    Media{Container: ContainerMP4, Video: &Video{Codec: "h264", PixFmt: "yuv420p", Profile: "High"}, Audio: &Audio{Codec: "aac"}},
			want: ModeDirect,
		},
		{
			name: "webm vp9 opus direct",
			m:    Media{Container: ContainerWebM, Video: &Video{Codec: "vp9", PixFmt: "yuv420p"}, Audio: &Audio{Codec: "opus"}},
			want: ModeDirect,
		},
		{
			name: "mp4 ac3 audio remuxes the video",
			m:    Media{Container: ContainerMP4, Video: &Video{Codec: "h264", PixFmt: "yuv420p", Profile: "High"}, Audio: &Audio{Codec: "ac3"}},
			want: ModeRemux,
			copy: true,
		},
		{
			name: "mkv h264 aac needs a video transcode for reliable seeking",
			m:    Media{Container: ContainerMatroska, Video: &Video{Codec: "h264", PixFmt: "yuv420p", Profile: "High"}, Audio: &Audio{Codec: "aac"}},
			want: ModeTranscode,
		},
		{
			name: "mkv h264 dts needs a video transcode",
			m:    Media{Container: ContainerMatroska, Video: &Video{Codec: "h264", PixFmt: "yuv420p", Profile: "High"}, Audio: &Audio{Codec: "dts"}},
			want: ModeTranscode,
		},
		{
			name: "hevc needs a video transcode",
			m:    Media{Container: ContainerMatroska, Video: &Video{Codec: "hevc", PixFmt: "yuv420p10le"}, Audio: &Audio{Codec: "aac"}},
			want: ModeTranscode,
		},
		{
			name: "h264 high 10 needs a video transcode",
			m:    Media{Container: ContainerMP4, Video: &Video{Codec: "h264", PixFmt: "yuv420p10le", Profile: "High 10"}, Audio: &Audio{Codec: "aac"}},
			want: ModeTranscode,
		},
		{
			name: "webm vp9 with flac audio cannot be copied into hls",
			m:    Media{Container: ContainerWebM, Video: &Video{Codec: "vp9", PixFmt: "yuv420p"}, Audio: &Audio{Codec: "flac"}},
			want: ModeTranscode,
		},
		{
			name: "mp4 h264 without audio is direct",
			m:    Media{Container: ContainerMP4, Video: &Video{Codec: "h264", PixFmt: "yuv420p", Profile: "Main"}},
			want: ModeDirect,
		},
		{
			name: "unknown video codec is transcoded",
			m:    Media{Container: ContainerMP4, Video: &Video{Codec: "mpeg2video", PixFmt: "yuv420p"}, Audio: &Audio{Codec: "aac"}},
			want: ModeTranscode,
		},
		{
			name: "avi h264 is transcoded rather than played directly",
			m:    Media{Container: ContainerOther, Video: &Video{Codec: "h264", PixFmt: "yuv420p", Profile: "High"}, Audio: &Audio{Codec: "aac"}},
			want: ModeTranscode,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := PlanFor(tc.m)
			if got.Mode != tc.want {
				t.Fatalf("mode = %q, want %q (%s)", got.Mode, tc.want, got.Reason)
			}
			if got.VideoCopy != tc.copy {
				t.Fatalf("VideoCopy = %v, want %v", got.VideoCopy, tc.copy)
			}
		})
	}
}

func TestClassifyContainer(t *testing.T) {
	for _, tc := range []struct {
		format, name string
		want         Container
	}{
		{"mov,mp4,m4a,3gp,3g2,mj2", "Film.mp4", ContainerMP4},
		{"mov,mp4,m4a,3gp,3g2,mj2", "Film.m4v", ContainerMP4},
		{"matroska,webm", "Film.webm", ContainerWebM},
		{"matroska,webm", "Film.mkv", ContainerMatroska},
		{"avi", "Film.avi", ContainerOther},
		{"", "Film.mkv", ContainerMatroska},
		{"", "Film.mp4", ContainerMP4},
	} {
		if got := ClassifyContainer(tc.format, tc.name); got != tc.want {
			t.Errorf("ClassifyContainer(%q, %q) = %q, want %q", tc.format, tc.name, got, tc.want)
		}
	}
}
