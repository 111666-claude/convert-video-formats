package presets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vib795/convert-video-formats/internal/probe"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "presets.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

const validConfig = `{
  "rules": [
    {
      "name": "vertical-shorts",
      "match": {"orientation": "portrait", "max_duration": 120, "has_audio": true},
      "preset": {"container": "mp4", "video_codec": "libx264", "max_scale_width": 1080, "max_scale_height": 1920, "quality": "medium"}
    },
    {
      "name": "silent-clips",
      "match": {"has_audio": false, "max_duration": 60},
      "preset": {"container": "webm", "video_codec": "libvpx-vp9", "max_scale_width": 1280, "quality": "low"}
    },
    {
      "name": "4k-landscape",
      "match": {"orientation": "landscape", "min_width": 3000},
      "preset": {"container": "mkv", "video_codec": "libx264", "max_scale_width": 1920, "max_scale_height": 1080, "quality": "high"}
    }
  ]
}`

func TestLoadValidConfig(t *testing.T) {
	cfg, err := Load(writeConfig(t, validConfig))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(cfg.Rules) != 3 {
		t.Fatalf("got %d rules, want 3", len(cfg.Rules))
	}
	// Default quality is filled in during validation.
	if cfg.Rules[1].Preset.Quality != "low" {
		t.Errorf("quality = %q, want low", cfg.Rules[1].Preset.Quality)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected error for missing config file")
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	_, err := Load(writeConfig(t, `{"rules": [`))
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("expected parse error, got: %v", err)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	content := `{
      "rules": [
        {"name": "x", "match": {}, "preset": {"container": "mp4", "video_codec": "libx264", "bogus": 1}}
      ]
    }`
	_, err := Load(writeConfig(t, content))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown-field error, got: %v", err)
	}
}

func TestValidateDuplicateRuleNames(t *testing.T) {
	content := `{
      "rules": [
        {"name": "dup", "match": {}, "preset": {"container": "mp4", "video_codec": "libx264"}},
        {"name": "dup", "match": {}, "preset": {"container": "mp4", "video_codec": "libx264"}}
      ]
    }`
	_, err := Load(writeConfig(t, content))
	if err == nil || !strings.Contains(err.Error(), "duplicate rule name") {
		t.Fatalf("expected duplicate-name error, got: %v", err)
	}
}

func TestValidateMissingRuleName(t *testing.T) {
	content := `{"rules": [{"name": "  ", "match": {}, "preset": {"container": "mp4", "video_codec": "libx264"}}]}`
	if _, err := Load(writeConfig(t, content)); err == nil {
		t.Fatal("expected missing-name error")
	}
}

func TestValidateIllegalScale(t *testing.T) {
	cases := map[string]string{
		"odd width": `{"container": "mp4", "video_codec": "libx264", "max_scale_width": 1081}`,
		"zero-ish":  `{"container": "mp4", "video_codec": "libx264", "max_scale_height": 1}`,
		"negative":  `{"container": "mp4", "video_codec": "libx264", "max_scale_width": -4}`,
	}
	for name, preset := range cases {
		t.Run(name, func(t *testing.T) {
			content := `{"rules": [{"name": "r", "match": {}, "preset": ` + preset + `}]}`
			_, err := Load(writeConfig(t, content))
			if err == nil || !strings.Contains(err.Error(), "positive even number") {
				t.Fatalf("expected illegal-scale error, got: %v", err)
			}
		})
	}
}

func TestValidateBadBoundsAndValues(t *testing.T) {
	cases := map[string]string{
		"min greater than max width":    `{"rules": [{"name": "r", "match": {"min_width": 2000, "max_width": 1000}, "preset": {"container": "mp4", "video_codec": "libx264"}}]}`,
		"min greater than max fps":      `{"rules": [{"name": "r", "match": {"min_fps": 60, "max_fps": 30}, "preset": {"container": "mp4", "video_codec": "libx264"}}]}`,
		"min greater than max duration": `{"rules": [{"name": "r", "match": {"min_duration": 10, "max_duration": 5}, "preset": {"container": "mp4", "video_codec": "libx264"}}]}`,
		"bad orientation":               `{"rules": [{"name": "r", "match": {"orientation": "sideways"}, "preset": {"container": "mp4", "video_codec": "libx264"}}]}`,
		"unsupported container":         `{"rules": [{"name": "r", "match": {}, "preset": {"container": "gif", "video_codec": "libx264"}}]}`,
		"unsupported video codec":       `{"rules": [{"name": "r", "match": {}, "preset": {"container": "mp4", "video_codec": "libx265"}}]}`,
		"unsupported audio codec":       `{"rules": [{"name": "r", "match": {}, "preset": {"container": "mp4", "video_codec": "libx264", "audio_codec": "mp3"}}]}`,
		"bad quality":                   `{"rules": [{"name": "r", "match": {}, "preset": {"container": "mp4", "video_codec": "libx264", "quality": "ultra"}}]}`,
		"missing container":             `{"rules": [{"name": "r", "match": {}, "preset": {"video_codec": "libx264"}}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, body)); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestValidateEmptyRulesIsAllowed(t *testing.T) {
	cfg, err := Load(writeConfig(t, `{"rules": []}`))
	if err != nil {
		t.Fatalf("empty rules should be valid: %v", err)
	}
	decision := NewEngine(cfg).Decide(probe.MediaInfo{Width: 1920, Height: 1080, FPS: 30, Duration: 10})
	if decision.Matched {
		t.Fatal("decision must not match when no rules are configured")
	}
}

// Media infos mirroring the three fixture categories.
var (
	verticalInfo = probe.MediaInfo{
		Width: 3840, Height: 2160, FPS: 30, Duration: 45.23,
		HasAudio: true, Rotation: 90,
	}
	landscapeInfo = probe.MediaInfo{
		Width: 3840, Height: 2160, FPS: 59.94, Duration: 612.5,
		HasAudio: true,
	}
	silentInfo = probe.MediaInfo{
		Width: 1280, Height: 720, FPS: 24, Duration: 12,
		HasAudio: false,
	}
)

func TestDecidePerFixture(t *testing.T) {
	cfg, err := Load(writeConfig(t, validConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	engine := NewEngine(cfg)

	t.Run("vertical short matches first rule and downscales with swapped axes", func(t *testing.T) {
		d := engine.Decide(verticalInfo)
		if !d.Matched || d.RuleName != "vertical-shorts" {
			t.Fatalf("got match=%t rule=%q", d.Matched, d.RuleName)
		}
		if d.Preset.Container != "mp4" || d.Preset.VideoCodec != "libx264" {
			t.Errorf("unexpected preset: %+v", d.Preset)
		}
		// Display is 2160x3840; cap 1080x1920 keeps aspect ratio.
		if d.Scale.NoScale || d.Scale.Width != 1080 || d.Scale.Height != 1920 {
			t.Errorf("scale = %+v, want 1080x1920", d.Scale)
		}
	})

	t.Run("4K landscape matches third rule after earlier rules fail", func(t *testing.T) {
		d := engine.Decide(landscapeInfo)
		if !d.Matched || d.RuleName != "4k-landscape" {
			t.Fatalf("got match=%t rule=%q", d.Matched, d.RuleName)
		}
		if d.Preset.Container != "mkv" {
			t.Errorf("container = %q, want mkv", d.Preset.Container)
		}
		if d.Scale.NoScale || d.Scale.Width != 1920 || d.Scale.Height != 1080 {
			t.Errorf("scale = %+v, want 1920x1080", d.Scale)
		}
		// The first two rules must show up as evaluated with reasons.
		if len(d.Evaluations) != 3 {
			t.Fatalf("got %d evaluations, want 3", len(d.Evaluations))
		}
		if d.Evaluations[0].Matched || len(d.Evaluations[0].Reasons) == 0 {
			t.Errorf("first rule should be skipped with reasons: %+v", d.Evaluations[0])
		}
	})

	t.Run("silent clip matches audio rule", func(t *testing.T) {
		d := engine.Decide(silentInfo)
		if !d.Matched || d.RuleName != "silent-clips" {
			t.Fatalf("got match=%t rule=%q", d.Matched, d.RuleName)
		}
		if d.Preset.Container != "webm" || d.Preset.VideoCodec != "libvpx-vp9" {
			t.Errorf("unexpected preset: %+v", d.Preset)
		}
		if d.EffectiveAudioCodec() != "libopus" {
			t.Errorf("default webm audio codec = %q, want libopus", d.EffectiveAudioCodec())
		}
		// 1280 wide source against a 1280 cap: no scaling.
		if !d.Scale.NoScale {
			t.Errorf("scale = %+v, want no scale", d.Scale)
		}
	})
}

func TestDecideNoMatchFallsBack(t *testing.T) {
	cfg, _ := Load(writeConfig(t, `{
      "rules": [
        {"name": "only-portrait", "match": {"orientation": "portrait"}, "preset": {"container": "mp4", "video_codec": "libx264"}}
      ]
    }`))
	d := NewEngine(cfg).Decide(landscapeInfo)
	if d.Matched {
		t.Fatal("landscape source must not match a portrait-only rule")
	}
	if len(d.Evaluations) != 1 || d.Evaluations[0].Matched {
		t.Fatalf("expected one failed evaluation, got %+v", d.Evaluations)
	}
	if !strings.Contains(d.Explain(), "no rule matched") {
		t.Errorf("explanation should state no rule matched:\n%s", d.Explain())
	}
}

func TestFirstMatchWinsAndOrderIsStable(t *testing.T) {
	cfg, _ := Load(writeConfig(t, `{
      "rules": [
        {"name": "catch-all-a", "match": {}, "preset": {"container": "mp4", "video_codec": "libx264"}},
        {"name": "catch-all-b", "match": {}, "preset": {"container": "mkv", "video_codec": "libx264"}}
      ]
    }`))
	d := NewEngine(cfg).Decide(silentInfo)
	if d.RuleName != "catch-all-a" {
		t.Fatalf("first rule must win, got %q", d.RuleName)
	}
}

func TestExplainIsDeterministic(t *testing.T) {
	cfg, _ := Load(writeConfig(t, validConfig))
	engine := NewEngine(cfg)
	first := engine.Decide(verticalInfo).Explain()
	second := engine.Decide(verticalInfo).Explain()
	if first != second {
		t.Fatal("explanation is not deterministic across runs")
	}
	if !strings.Contains(first, "vertical-shorts") || !strings.Contains(first, "MATCH") {
		t.Errorf("explanation missing match details:\n%s", first)
	}
}

func TestResolveScaleNeverUpscales(t *testing.T) {
	p := Preset{MaxScaleWidth: 4000, MaxScaleHeight: 4000}
	scale := resolveScale(1280, 720, p)
	if !scale.NoScale {
		t.Errorf("small source must not be upscaled, got %+v", scale)
	}
}

func TestResolveScaleKeepsAspectAndEvenPixels(t *testing.T) {
	// 1920x1080 capped to width 1000 -> 1000x562 (even, ratio preserved).
	scale := resolveScale(1920, 1080, Preset{MaxScaleWidth: 1000})
	if scale.NoScale || scale.Width != 1000 || scale.Height != 562 {
		t.Fatalf("scale = %+v, want 1000x562", scale)
	}
}
