// Package presets implements ordered, conditional transcoding rules.
//
// Rules are evaluated in the order they appear in the config file; the first
// rule whose match conditions all hold wins. Every decision records which
// rules were evaluated and why they did or did not match, so decisions are
// deterministic and explainable.
package presets

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Match describes the conditions a media file must satisfy for a rule.
// Every field is optional: a zero-valued field imposes no constraint, so a
// rule with an empty Match block matches every file.
type Match struct {
	// Orientation is "portrait", "landscape" or "square", compared
	// against the display (post-rotation) dimensions.
	Orientation string `json:"orientation,omitempty"`
	// MinWidth / MaxWidth bound the display width in pixels.
	MinWidth int `json:"min_width,omitempty"`
	MaxWidth int `json:"max_width,omitempty"`
	// MinHeight / MaxHeight bound the display height in pixels.
	MinHeight int `json:"min_height,omitempty"`
	MaxHeight int `json:"max_height,omitempty"`
	// MinFPS / MaxFPS bound the average frame rate.
	MinFPS float64 `json:"min_fps,omitempty"`
	MaxFPS float64 `json:"max_fps,omitempty"`
	// MinDuration / MaxDuration bound the duration in seconds.
	MinDuration float64 `json:"min_duration,omitempty"`
	MaxDuration float64 `json:"max_duration,omitempty"`
	// HasAudio, when non-nil, requires the presence or absence of an
	// audio stream.
	HasAudio *bool `json:"has_audio,omitempty"`
}

// Preset describes the transcoding parameters applied by a matching rule.
type Preset struct {
	// Container is the target container/extension, e.g. "mp4" or "webm".
	Container string `json:"container"`
	// VideoCodec is the ffmpeg video encoder name, e.g. "libx264".
	VideoCodec string `json:"video_codec"`
	// AudioCodec is the ffmpeg audio encoder name. Empty means "aac"
	// for non-WebM containers and "libopus" for WebM.
	AudioCodec string `json:"audio_codec,omitempty"`
	// MaxScaleWidth / MaxScaleHeight cap the display dimensions. The
	// video is only ever downscaled, never upscaled, and aspect ratio is
	// preserved. Both must be positive even numbers when set.
	MaxScaleWidth  int `json:"max_scale_width,omitempty"`
	MaxScaleHeight int `json:"max_scale_height,omitempty"`
	// Quality is high, medium or low and maps to encoder-specific
	// rate-control settings. Defaults to medium.
	Quality string `json:"quality,omitempty"`
}

// Rule is one ordered conditional transcoding rule.
type Rule struct {
	Name   string `json:"name"`
	Match  Match  `json:"match"`
	Preset Preset `json:"preset"`
}

// Config is the full preset file.
type Config struct {
	Rules []Rule `json:"rules"`
}

// supportedContainers and codecs mirror what buildFFmpegArgs already handles.
var supportedContainers = map[string]bool{
	"mp4": true, "avi": true, "mov": true,
	"mkv": true, "webm": true, "flv": true,
}

var supportedVideoCodecs = map[string]bool{
	"libx264": true, "libvpx-vp9": true,
}

var supportedAudioCodecs = map[string]bool{
	"aac": true, "libopus": true,
}

var supportedOrientations = map[string]bool{
	"portrait": true, "landscape": true, "square": true,
}

// Load reads and validates a preset config file. An empty rules list is
// valid and means no conditional rule applies (the caller falls back to the
// original conversion behavior).
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read preset config %s: %w", path, err)
	}

	var cfg Config
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse preset config %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid preset config %s: %w", path, err)
	}
	return &cfg, nil
}

// Validate checks the whole config: unique rule names, sane match bounds and
// legal preset values.
func (cfg *Config) Validate() error {
	names := make(map[string]bool, len(cfg.Rules))
	for i := range cfg.Rules {
		rule := &cfg.Rules[i]
		if strings.TrimSpace(rule.Name) == "" {
			return fmt.Errorf("rule at index %d is missing a name", i)
		}
		if names[rule.Name] {
			return fmt.Errorf("duplicate rule name %q: rule names must be unique", rule.Name)
		}
		names[rule.Name] = true

		if err := rule.Match.validate(rule.Name); err != nil {
			return err
		}
		if err := rule.Preset.validate(rule.Name); err != nil {
			return err
		}
	}
	return nil
}

func (m Match) validate(ruleName string) error {
	label := fmt.Sprintf("rule %q", ruleName)

	if m.Orientation != "" && !supportedOrientations[strings.ToLower(m.Orientation)] {
		return fmt.Errorf("%s: invalid orientation %q (want portrait, landscape or square)", label, m.Orientation)
	}
	if m.MinWidth < 0 || m.MaxWidth < 0 || m.MinHeight < 0 || m.MaxHeight < 0 {
		return fmt.Errorf("%s: dimension bounds must not be negative", label)
	}
	if m.MinWidth > 0 && m.MaxWidth > 0 && m.MinWidth > m.MaxWidth {
		return fmt.Errorf("%s: min_width (%d) is greater than max_width (%d)", label, m.MinWidth, m.MaxWidth)
	}
	if m.MinHeight > 0 && m.MaxHeight > 0 && m.MinHeight > m.MaxHeight {
		return fmt.Errorf("%s: min_height (%d) is greater than max_height (%d)", label, m.MinHeight, m.MaxHeight)
	}
	if m.MinFPS < 0 || m.MaxFPS < 0 || m.MinDuration < 0 || m.MaxDuration < 0 {
		return fmt.Errorf("%s: frame rate and duration bounds must not be negative", label)
	}
	if m.MinFPS > 0 && m.MaxFPS > 0 && m.MinFPS > m.MaxFPS {
		return fmt.Errorf("%s: min_fps (%g) is greater than max_fps (%g)", label, m.MinFPS, m.MaxFPS)
	}
	if m.MinDuration > 0 && m.MaxDuration > 0 && m.MinDuration > m.MaxDuration {
		return fmt.Errorf("%s: min_duration (%g) is greater than max_duration (%g)", label, m.MinDuration, m.MaxDuration)
	}
	return nil
}

func (p *Preset) validate(ruleName string) error {
	label := fmt.Sprintf("rule %q", ruleName)

	p.Container = strings.ToLower(strings.TrimSpace(p.Container))
	p.VideoCodec = strings.TrimSpace(p.VideoCodec)
	p.AudioCodec = strings.TrimSpace(p.AudioCodec)

	if p.Container == "" {
		return fmt.Errorf("%s: preset is missing a container", label)
	}
	if !supportedContainers[p.Container] {
		return fmt.Errorf("%s: unsupported container %q", label, p.Container)
	}
	if p.VideoCodec == "" {
		return fmt.Errorf("%s: preset is missing a video_codec", label)
	}
	if !supportedVideoCodecs[p.VideoCodec] {
		return fmt.Errorf("%s: unsupported video codec %q", label, p.VideoCodec)
	}
	if p.AudioCodec != "" && !supportedAudioCodecs[p.AudioCodec] {
		return fmt.Errorf("%s: unsupported audio codec %q", label, p.AudioCodec)
	}
	if p.Quality == "" {
		p.Quality = "medium"
	}
	p.Quality = strings.ToLower(p.Quality)
	if p.Quality != "high" && p.Quality != "medium" && p.Quality != "low" {
		return fmt.Errorf("%s: invalid quality %q (want high, medium or low)", label, p.Quality)
	}
	if err := validateEvenScale(label, "max_scale_width", p.MaxScaleWidth); err != nil {
		return err
	}
	if err := validateEvenScale(label, "max_scale_height", p.MaxScaleHeight); err != nil {
		return err
	}
	return nil
}

// validateEvenScale allows 0 (unset) but rejects negatives, odd values and
// values below 2.
func validateEvenScale(label, field string, value int) error {
	if value == 0 {
		return nil
	}
	if value < 2 || value%2 != 0 {
		return fmt.Errorf("%s: %s must be a positive even number, got %d", label, field, value)
	}
	return nil
}
