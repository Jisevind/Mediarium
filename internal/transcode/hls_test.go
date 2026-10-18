package transcode

import (
	"strings"
	"testing"
)

func TestSegmentsFollowKeyframesAndTargetDuration(t *testing.T) {
	got := Segments([]float64{0, 2, 4, 6, 8.1, 12}, 12, 4)
	want := []Segment{
		{Index: 0, Start: 0, End: 4},
		{Index: 1, Start: 4, End: 8.1},
		{Index: 2, Start: 8.1, End: 12},
	}
	if len(got) != len(want) {
		t.Fatalf("segments = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("segment %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestSegmentsFallBackToFixedBoundaries(t *testing.T) {
	got := Segments(nil, 10, 4)
	want := []Segment{
		{Index: 0, Start: 0, End: 4},
		{Index: 1, Start: 4, End: 8},
		{Index: 2, Start: 8, End: 10},
	}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("segments = %+v, want %+v", got, want)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("segments = %+v, want %+v", got, want)
	}
}

func TestSegmentsKeepSparseKeyframesWhole(t *testing.T) {
	// A file whose only keyframe is at the start cannot be cut anywhere else
	// with a stream copy; one segment is correct, and the player will still
	// seek once it has been made.
	got := Segments([]float64{0}, 10, 4)
	if len(got) != 1 || got[0] != (Segment{Index: 0, Start: 0, End: 10}) {
		t.Fatalf("segments = %+v", got)
	}
}

func TestPlaylistIsAVODPlaylistWithRelativeSegments(t *testing.T) {
	got := Playlist([]Segment{{Index: 0, Start: 0, End: 4}, {Index: 1, Start: 4, End: 8.5}})
	for _, want := range []string{
		"#EXTM3U",
		"#EXT-X-PLAYLIST-TYPE:VOD",
		"#EXT-X-INDEPENDENT-SEGMENTS",
		"#EXT-X-TARGETDURATION:5",
		"#EXTINF:4.000,",
		"seg-0.ts",
		"#EXTINF:4.500,",
		"seg-1.ts",
		"#EXT-X-ENDLIST",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("playlist is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "/api/") {
		t.Errorf("segment addresses must stay relative to the playlist:\n%s", got)
	}
}

func TestSegmentArgsCopyVideoAndAlwaysMakeAAC(t *testing.T) {
	plan := Plan{Mode: ModeRemux, VideoCopy: true}
	args := SegmentArgs("/movies/Film (2020)/Film.mkv", Segment{Index: 2, Start: 8, End: 12}, plan, Encoder{Name: "libx264", Args: []string{"-c:v", "libx264"}}, "/tmp/seg-2.ts")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-ss 8.000", "-t 4.000", "-i /movies/Film (2020)/Film.mkv",
		"-map 0:v:0?", "-map 0:a:0?", "-c:v copy", "-c:a aac", "-ac 2",
		"-output_ts_offset 8.000", "-f mpegts", "/tmp/seg-2.ts",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args are missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "libx264") {
		t.Errorf("a remux must not encode the video:\n%s", joined)
	}
}

func TestSegmentArgsMapTheChosenAudioStream(t *testing.T) {
	plan := Plan{Mode: ModeTranscode, AudioIndex: 1}
	args := SegmentArgs("/movies/Film.mkv", Segment{Index: 0, Start: 0, End: 4}, plan, SoftwareEncoder(), "/tmp/seg-0.ts")
	if joined := strings.Join(args, " "); !strings.Contains(joined, "-map 0:a:1?") {
		t.Fatalf("the chosen audio stream is missing:\n%s", joined)
	}
}

func TestSegmentArgsEncodeVideoWhenNeeded(t *testing.T) {
	plan := Plan{Mode: ModeTranscode}
	args := SegmentArgs("/movies/Film.mkv", Segment{Index: 0, Start: 0, End: 4}, plan, Encoder{Name: "libx264", Args: []string{"-c:v", "libx264", "-preset", "veryfast"}}, "/tmp/seg-0.ts")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-c:v libx264 -preset veryfast") {
		t.Fatalf("video encoder args missing:\n%s", joined)
	}
}
