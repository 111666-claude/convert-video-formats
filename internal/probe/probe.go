// Package probe inspects video files with ffprobe and returns the metadata
// needed for conditional transcoding decisions.
package probe

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// MediaInfo is the subset of ffprobe metadata used by the preset engine.
type MediaInfo struct {
	// Width and Height are the stored (coded) dimensions, before any
	// rotation is applied.
	Width  int
	Height int
	// FPS is the average frame rate in frames per second.
	FPS float64
	// Duration is the container duration in seconds.
	Duration float64
	// HasAudio reports whether the file contains at least one audio stream.
	HasAudio bool
	// Rotation is the display rotation in degrees, normalized to
	// 0, 90, 180 or 270. Rotation metadata is preserved on transcode.
	Rotation int
}

// DisplayWidth and DisplayHeight return the dimensions after applying the
// rotation metadata. A 90/270 degree rotation swaps the stored dimensions.
func (m MediaInfo) DisplayWidth() int {
	if m.Rotation == 90 || m.Rotation == 270 {
		return m.Height
	}
	return m.Width
}

// DisplayHeight returns the display (post-rotation) height.
func (m MediaInfo) DisplayHeight() int {
	if m.Rotation == 90 || m.Rotation == 270 {
		return m.Width
	}
	return m.Height
}

// Prober inspects a media file and returns its metadata.
type Prober interface {
	Probe(path string) (MediaInfo, error)
}

// FFprobe runs the real ffprobe binary. The Binary field may be overridden
// (for example with a fake ffprobe in tests); it defaults to "ffprobe".
type FFprobe struct {
	Binary string
}

// NewFFprobe returns a Prober backed by the ffprobe binary on PATH.
func NewFFprobe() *FFprobe {
	return &FFprobe{Binary: "ffprobe"}
}

// ffprobeJSON mirrors the relevant parts of ffprobe's JSON output.
type ffprobeJSON struct {
	Streams []struct {
		CodecType  string            `json:"codec_type"`
		Width      int               `json:"width"`
		Height     int               `json:"height"`
		AvgFrame   string            `json:"avg_frame_rate"`
		RFrameRate string            `json:"r_frame_rate"`
		Duration   string            `json:"duration"`
		SideData   []json.RawMessage `json:"side_data_list"`
		Tags       map[string]string `json:"tags"`
	} `json:"streams"`
	Frames []struct {
		SideData []json.RawMessage `json:"side_data_list"`
	} `json:"frames"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// Probe runs ffprobe on path and converts its output into a MediaInfo.
func (f *FFprobe) Probe(path string) (MediaInfo, error) {
	binary := f.Binary
	if binary == "" {
		binary = "ffprobe"
	}

	cmd := exec.Command(binary,
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		"-show_frames",
		path,
	)
	raw, err := cmd.Output()
	if err != nil {
		return MediaInfo{}, fmt.Errorf("ffprobe failed for %s: %w", path, err)
	}

	var parsed ffprobeJSON
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return MediaInfo{}, fmt.Errorf("ffprobe returned invalid JSON for %s: %w", path, err)
	}

	return parseMediaInfo(path, parsed)
}

// parseMediaInfo converts ffprobe JSON into MediaInfo and reports any
// required field that is missing or unparseable as an explicit error.
func parseMediaInfo(path string, parsed ffprobeJSON) (MediaInfo, error) {
	var info MediaInfo

	var videoIndex = -1
	for i, s := range parsed.Streams {
		if s.CodecType == "video" {
			videoIndex = i
			break
		}
	}
	if videoIndex == -1 {
		return MediaInfo{}, fmt.Errorf("probe data missing required field: no video stream in %s", path)
	}
	v := parsed.Streams[videoIndex]

	for _, s := range parsed.Streams {
		if s.CodecType == "audio" {
			info.HasAudio = true
			break
		}
	}

	if v.Width <= 0 {
		return MediaInfo{}, fmt.Errorf("probe data missing required field: width for %s", path)
	}
	if v.Height <= 0 {
		return MediaInfo{}, fmt.Errorf("probe data missing required field: height for %s", path)
	}
	info.Width = v.Width
	info.Height = v.Height

	fps, err := parseRate(v.AvgFrame, v.RFrameRate)
	if err != nil {
		return MediaInfo{}, fmt.Errorf("probe data missing required field: frame rate for %s (%w)", path, err)
	}
	info.FPS = fps

	duration, err := parseDuration(v.Duration, parsed.Format.Duration)
	if err != nil {
		return MediaInfo{}, fmt.Errorf("probe data missing required field: duration for %s (%w)", path, err)
	}
	info.Duration = duration

	rotation, err := parseRotation(v.SideData, frameSideData(parsed), v.Tags)
	if err != nil {
		return MediaInfo{}, fmt.Errorf("invalid rotation metadata for %s: %w", path, err)
	}
	info.Rotation = rotation

	return info, nil
}

// frameSideData collects side data entries from the first probed frames.
// Some encoders (e.g. an H.264 SEI display orientation) attach the display
// matrix per frame rather than to the stream.
func frameSideData(parsed ffprobeJSON) []json.RawMessage {
	for _, frame := range parsed.Frames {
		if len(frame.SideData) > 0 {
			return frame.SideData
		}
	}
	return nil
}

// parseRate parses an ffprobe frame-rate fraction such as "30000/1001".
// rFrameRate is used as a fallback when avg_frame_rate is absent or 0/0.
func parseRate(avg, rFrame string) (float64, error) {
	for _, rate := range []string{avg, rFrame} {
		if rate == "" || rate == "0/0" || rate == "N/A" {
			continue
		}
		if num, den, ok := strings.Cut(rate, "/"); ok {
			n, err1 := strconv.ParseFloat(num, 64)
			d, err2 := strconv.ParseFloat(den, 64)
			if err1 == nil && err2 == nil && d != 0 {
				return n / d, nil
			}
		}
		if f, err := strconv.ParseFloat(rate, 64); err == nil && f > 0 {
			return f, nil
		}
	}
	return 0, fmt.Errorf("no usable frame rate")
}

// parseDuration reads the duration of the video stream, falling back to the
// container-level duration.
func parseDuration(streamDuration, formatDuration string) (float64, error) {
	for _, candidate := range []string{streamDuration, formatDuration} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || candidate == "N/A" {
			continue
		}
		if d, err := strconv.ParseFloat(candidate, 64); err == nil && d > 0 {
			return d, nil
		}
	}
	return 0, fmt.Errorf("no usable duration")
}

// rotationSideData mirrors ffprobe's display matrix side data entry.
type rotationSideData struct {
	SideDataType string `json:"side_data_type"`
	Rotation     int    `json:"rotation"`
}

// parseRotation extracts display rotation from stream-level display matrix
// side data, frame-level side data, or legacy rotate tags, normalizing to
// 0/90/180/270 degrees.
func parseRotation(streamSideData, frameSideDataList []json.RawMessage, tags map[string]string) (int, error) {
	degrees := 0
	found := false

	for _, raw := range append(append([]json.RawMessage{}, streamSideData...), frameSideDataList...) {
		var entry rotationSideData
		if err := json.Unmarshal(raw, &entry); err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(entry.SideDataType), "display matrix") ||
			strings.Contains(strings.ToLower(entry.SideDataType), "displaymatrix") ||
			strings.Contains(strings.ToLower(entry.SideDataType), "rotation") {
			degrees = entry.Rotation
			found = true
			break
		}
	}

	if !found {
		for key, value := range tags {
			if strings.EqualFold(key, "rotate") {
				parsed, err := strconv.Atoi(strings.TrimSpace(value))
				if err != nil {
					return 0, fmt.Errorf("unparseable rotate tag %q", value)
				}
				degrees = parsed
				found = true
				break
			}
		}
	}

	if !found {
		return 0, nil
	}

	// ffprobe reports counter-clockwise rotation as a negative value.
	normalized := ((degrees % 360) + 360) % 360
	switch normalized {
	case 0, 90, 180, 270:
		return normalized, nil
	default:
		return 0, fmt.Errorf("rotation %d is not a multiple of 90 degrees", degrees)
	}
}
