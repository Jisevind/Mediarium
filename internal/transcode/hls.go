package transcode

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Segment is one HLS segment: a keyframe-aligned slice of the file.
type Segment struct {
	Index int
	Start float64
	End   float64
}

// Segments cuts a file at its keyframes, so a segment can be encoded on its
// own and seeking to any point starts at a real keyframe. Keyframes that are
// closer together than target are skipped; one keyframe per segment is enough.
// Without keyframe times it falls back to fixed target-length segments.
func Segments(keyframes []float64, duration, target float64) []Segment {
	if duration <= 0 {
		return nil
	}
	if target <= 0 {
		target = 4
	}
	var cuts []float64
	for _, k := range keyframes {
		if k > 0 && k < duration {
			cuts = append(cuts, k)
		}
	}
	sort.Float64s(cuts)
	boundaries := []float64{0}
	last := 0.0
	if len(cuts) == 0 && len(keyframes) == 0 {
		for start := target; start < duration; start += target {
			boundaries = append(boundaries, start)
			last = start
		}
	} else if len(cuts) > 0 {
		for _, k := range cuts {
			if k-last >= target {
				boundaries = append(boundaries, k)
				last = k
			}
		}
	}
	if last < duration {
		boundaries = append(boundaries, duration)
	}
	out := make([]Segment, 0, len(boundaries)-1)
	for i := 0; i+1 < len(boundaries); i++ {
		out = append(out, Segment{Index: i, Start: boundaries[i], End: boundaries[i+1]})
	}
	return out
}

// Playlist is a complete VOD playlist. The segment addresses are relative, so
// the playlist keeps working under URL_BASE and behind a reverse proxy.
func Playlist(segments []Segment) string {
	var b strings.Builder
	max := 1.0
	for _, s := range segments {
		if d := s.End - s.Start; d > max {
			max = d
		}
	}
	target := int(math.Ceil(max))
	if target < 1 {
		target = 1
	}
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	fmt.Fprintf(&b, "#EXT-X-TARGETDURATION:%d\n", target)
	b.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	b.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")
	b.WriteString("#EXT-X-INDEPENDENT-SEGMENTS\n")
	for _, s := range segments {
		fmt.Fprintf(&b, "#EXTINF:%.3f,\nseg-%d.ts\n", s.End-s.Start, s.Index)
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

// SegmentArgs is the ffmpeg command line for one segment. The video is copied
// when the plan says the source H.264 can go into MPEG-TS as it is; otherwise
// the encoder arguments are used. Audio always becomes stereo AAC, which is
// what fixes a file whose video played but whose sound did not.
func SegmentArgs(input string, seg Segment, plan Plan, enc Encoder, output string) []string {
	args := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-ss", fmt.Sprintf("%.3f", seg.Start),
		"-i", input,
		"-t", fmt.Sprintf("%.3f", seg.End-seg.Start),
		"-map", "0:v:0?", "-map", fmt.Sprintf("0:a:%d?", plan.AudioIndex), "-sn", "-dn",
	}
	if plan.VideoCopy {
		args = append(args, "-c:v", "copy")
	} else {
		args = append(args, enc.Args...)
	}
	args = append(args,
		"-c:a", "aac", "-b:a", "160k", "-ac", "2",
		"-output_ts_offset", fmt.Sprintf("%.3f", seg.Start),
		"-muxdelay", "0", "-muxpreload", "0",
		"-f", "mpegts", output,
	)
	return args
}
