package transcode

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Encoder is how a video stream is turned into H.264. Args are the ffmpeg
// output options, so a plan can carry the encoder that was chosen for a
// session and every segment uses the same one.
type Encoder struct {
	Name string
	Args []string
}

// SoftwareEncoder is the fallback that works on every machine.
func SoftwareEncoder() Encoder {
	return Encoder{Name: "libx264", Args: []string{"-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-pix_fmt", "yuv420p"}}
}

// AvailableEncoders lists the video encoders the ffmpeg binary knows. It is
// read from ffmpeg itself, so a user's own build works without a list here.
func AvailableEncoders(ctx context.Context, ffmpegPath string) (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, ffmpegPath, "-hide_banner", "-encoders").Output()
	if err != nil {
		return nil, fmt.Errorf("list ffmpeg encoders: %w", err)
	}
	found := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.HasPrefix(fields[0], "V") {
			found[fields[1]] = true
		}
	}
	return found, nil
}

// ChooseEncoder picks a hardware encoder when one is available and works, or
// libx264 otherwise. "auto" tries VAAPI, QSV and NVENC in that order. An
// explicit choice that cannot run is an error, which the caller turns into
// direct play or a software fallback; it never silently pretends the chosen
// encoder is in use.
func ChooseEncoder(ctx context.Context, ffmpegPath, preference string, vaapiDevice string) (Encoder, error) {
	preference = strings.ToLower(strings.TrimSpace(preference))
	if preference == "" {
		preference = "auto"
	}
	if preference == "software" {
		return SoftwareEncoder(), nil
	}
	available, err := AvailableEncoders(ctx, ffmpegPath)
	if err != nil {
		if preference == "auto" {
			return SoftwareEncoder(), nil
		}
		return Encoder{}, err
	}
	var order []string
	switch preference {
	case "auto":
		order = []string{"vaapi", "qsv", "nvenc"}
	case "vaapi", "qsv", "nvenc":
		order = []string{preference}
	default:
		return Encoder{}, fmt.Errorf("unknown hardware encoder %q", preference)
	}
	for _, kind := range order {
		enc, ok := encoderFor(kind, available, vaapiDevice)
		if !ok {
			continue
		}
		if testEncoder(ctx, ffmpegPath, enc) {
			return enc, nil
		}
	}
	if preference == "auto" {
		return SoftwareEncoder(), nil
	}
	return Encoder{}, fmt.Errorf("the %s encoder isn't available to ffmpeg", preference)
}

func encoderFor(kind string, available map[string]bool, vaapiDevice string) (Encoder, bool) {
	switch kind {
	case "vaapi":
		device := vaapiDevice
		if device == "" {
			devices, _ := filepath.Glob("/dev/dri/renderD*")
			if len(devices) > 0 {
				device = devices[0]
			}
		}
		if device == "" || !available["h264_vaapi"] {
			return Encoder{}, false
		}
		return Encoder{Name: "h264_vaapi", Args: []string{"-vaapi_device", device, "-vf", "format=nv12,hwupload", "-c:v", "h264_vaapi", "-qp", "23"}}, true
	case "qsv":
		if !available["h264_qsv"] {
			return Encoder{}, false
		}
		return Encoder{Name: "h264_qsv", Args: []string{"-c:v", "h264_qsv", "-preset", "veryfast", "-global_quality", "23"}}, true
	case "nvenc":
		if !available["h264_nvenc"] {
			return Encoder{}, false
		}
		return Encoder{Name: "h264_nvenc", Args: []string{"-c:v", "h264_nvenc", "-preset", "p4", "-cq", "23", "-b:v", "0"}}, true
	}
	return Encoder{}, false
}

// testEncoder runs one tiny frame through the encoder. An encoder can be
// compiled into ffmpeg and still fail at runtime when the device or driver is
// missing, so presence in -encoders alone is not enough.
func testEncoder(ctx context.Context, ffmpegPath string, enc Encoder) bool {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin",
		"-f", "lavfi", "-i", "color=c=black:s=64x64:d=0.2", "-frames:v", "1"}
	args = append(args, enc.Args...)
	args = append(args, "-f", "null", "-")
	return exec.CommandContext(ctx, ffmpegPath, args...).Run() == nil
}
