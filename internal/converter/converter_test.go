package converter

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vib795/convert-video-formats/internal/presets"
	"github.com/vib795/convert-video-formats/pkg/types"
)

const testPresetConfig = `{
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

// fakeBinDir returns the testdata directory holding the fake ffmpeg/ffprobe
// and puts it first on PATH for the duration of the test.
func fakeBinDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", "fakes"))
	if err != nil {
		t.Fatalf("resolve fake bin dir: %v", err)
	}
	if _, err := exec.LookPath(filepath.Join(dir, "ffmpeg")); err != nil {
		t.Fatalf("fake ffmpeg not executable: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func newTestConverter(t *testing.T, opts *types.ConversionOptions, withEngine bool) (*Converter, string) {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "presets.json")
	if err := os.WriteFile(cfgPath, []byte(testPresetConfig), 0644); err != nil {
		t.Fatalf("write preset config: %v", err)
	}

	conv := NewConverter(opts)
	if withEngine {
		cfg, err := presets.Load(cfgPath)
		if err != nil {
			t.Fatalf("load preset config: %v", err)
		}
		conv.WithPresets(presets.NewEngine(cfg))
	}
	return conv, cfgPath
}

func baseOptions(t *testing.T) *types.ConversionOptions {
	return &types.ConversionOptions{
		Format:    "mp4",
		Quality:   "medium",
		Overwrite: false,
	}
}

// joinArgs renders an argument list like a shell command for assertions.
func joinArgs(args []string) string {
	return strings.Join(args, " ")
}

func TestBuildPlanForThreeFixtureClasses(t *testing.T) {
	fakeBinDir(t)
	conv, _ := newTestConverter(t, baseOptions(t), true)

	t.Run("vertical short: rotation preserved, scale axes swapped, audio kept", func(t *testing.T) {
		input := filepath.Join("testdata", "fixtures", "vertical_short.mp4")
		plan, err := conv.buildPlan(input, "", false)
		if err != nil {
			t.Fatalf("buildPlan: %v", err)
		}
		if plan == nil || plan.decision.RuleName != "vertical-shorts" {
			t.Fatalf("unexpected plan: %+v", plan)
		}
		cmdline := joinArgs(plan.args)
		if !strings.Contains(cmdline, "-noautorotate") {
			t.Errorf("rotated input must pass -noautorotate to preserve rotation metadata:\n%s", cmdline)
		}
		// Stored frame is 3840x2160; display cap 1080x1920 swaps back.
		if !strings.Contains(cmdline, "scale=1920:1080:flags=lanczos") {
			t.Errorf("expected swapped scale=1920:1080 filter, got:\n%s", cmdline)
		}
		if !strings.Contains(cmdline, "-c:v libx264") {
			t.Errorf("expected libx264 encoder, got:\n%s", cmdline)
		}
		if !strings.Contains(cmdline, "-c:a aac") || strings.Contains(cmdline, " -an") {
			t.Errorf("clip with audio must encode an AAC track, got:\n%s", cmdline)
		}
		if filepath.Ext(plan.outputPath) != ".mp4" {
			t.Errorf("output extension = %q, want .mp4", filepath.Ext(plan.outputPath))
		}
	})

	t.Run("4K landscape: downscaled to 1080p into mkv", func(t *testing.T) {
		input := filepath.Join("testdata", "fixtures", "landscape_4k.mp4")
		plan, err := conv.buildPlan(input, "", false)
		if err != nil {
			t.Fatalf("buildPlan: %v", err)
		}
		if plan == nil || plan.decision.RuleName != "4k-landscape" {
			t.Fatalf("unexpected plan: %+v", plan)
		}
		cmdline := joinArgs(plan.args)
		if strings.Contains(cmdline, "-noautorotate") {
			t.Errorf("unrotated input must not pass -noautorotate:\n%s", cmdline)
		}
		if !strings.Contains(cmdline, "scale=1920:1080:flags=lanczos") {
			t.Errorf("expected scale=1920:1080 filter, got:\n%s", cmdline)
		}
		if !strings.Contains(cmdline, "-crf 18") || !strings.Contains(cmdline, "-preset slow") {
			t.Errorf("high quality tier missing, got:\n%s", cmdline)
		}
		if filepath.Ext(plan.outputPath) != ".mkv" {
			t.Errorf("output extension = %q, want .mkv", filepath.Ext(plan.outputPath))
		}
	})

	t.Run("silent clip: webm/vp9 with -an and no scaling", func(t *testing.T) {
		input := filepath.Join("testdata", "fixtures", "silent_clip.mov")
		plan, err := conv.buildPlan(input, "", false)
		if err != nil {
			t.Fatalf("buildPlan: %v", err)
		}
		if plan == nil || plan.decision.RuleName != "silent-clips" {
			t.Fatalf("unexpected plan: %+v", plan)
		}
		cmdline := joinArgs(plan.args)
		if !strings.Contains(cmdline, "-c:v libvpx-vp9") {
			t.Errorf("expected libvpx-vp9 encoder, got:\n%s", cmdline)
		}
		if !strings.Contains(cmdline, " -an") {
			t.Errorf("silent source must use -an, got:\n%s", cmdline)
		}
		if strings.Contains(cmdline, "-c:a") {
			t.Errorf("silent source must not request an audio codec, got:\n%s", cmdline)
		}
		if strings.Contains(cmdline, "-vf") {
			t.Errorf("source within caps must not be scaled, got:\n%s", cmdline)
		}
		if filepath.Ext(plan.outputPath) != ".webm" {
			t.Errorf("output extension = %q, want .webm", filepath.Ext(plan.outputPath))
		}
	})
}

func TestBuildPlanWithoutEngineReturnsNil(t *testing.T) {
	fakeBinDir(t)
	conv := NewConverter(baseOptions(t))
	plan, err := conv.buildPlan(filepath.Join("testdata", "fixtures", "silent_clip.mov"), "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan != nil {
		t.Fatalf("no preset engine means legacy path, got plan %+v", plan)
	}
}

func TestBuildPlanProbeFailureIsExplicit(t *testing.T) {
	fakeBinDir(t)
	conv, _ := newTestConverter(t, baseOptions(t), true)
	_, err := conv.buildPlan(filepath.Join("testdata", "fixtures", "probe_fail.mp4"), "", false)
	if err == nil || !strings.Contains(err.Error(), "cannot determine transcoding preset") {
		t.Fatalf("expected explicit probe error, got: %v", err)
	}
}

func TestBuildPlanNoMatchingRuleFallsBack(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "presets.json")
	if err := os.WriteFile(cfgPath, []byte(`{
      "rules": [
        {"name": "only-short", "match": {"max_duration": 1}, "preset": {"container": "mp4", "video_codec": "libx264"}}
      ]
    }`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := presets.Load(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	conv := NewConverter(baseOptions(t)).WithPresets(presets.NewEngine(cfg))
	fakeBinDir(t)
	plan, err := conv.buildPlan(filepath.Join("testdata", "fixtures", "landscape_4k.mp4"), "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan != nil {
		t.Fatalf("unmatched rules must fall back to legacy conversion, got %+v", plan)
	}
}

func TestConvertSingleFileWithPresetRunsPlannedCommand(t *testing.T) {
	fakeBinDir(t)
	logPath := filepath.Join(t.TempDir(), "ffmpeg.log")
	t.Setenv("FAKE_FFMPEG_LOG", logPath)

	workDir := t.TempDir()
	input := filepath.Join(workDir, "silent_clip.mov")
	if err := os.WriteFile(input, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(workDir, "out")

	opts := baseOptions(t)
	opts.InputPath = input
	conv, _ := newTestConverter(t, opts, true)

	plan, err := conv.buildPlan(input, outputDir, true)
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}
	if err := conv.runPlannedConversion(plan); err != nil {
		t.Fatalf("runPlannedConversion: %v", err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("fake ffmpeg was not invoked: %v", err)
	}
	logged := string(logBytes)
	if !strings.Contains(logged, "-c:v libvpx-vp9") || !strings.Contains(logged, " -an") {
		t.Errorf("logged command missing expected preset arguments:\n%s", logged)
	}
	if !strings.Contains(logged, filepath.Join("out", "silent_clip.webm")) {
		t.Errorf("logged command does not target the planned output:\n%s", logged)
	}
	if _, err := os.Stat(plan.outputPath); err != nil {
		t.Errorf("expected fake ffmpeg to create output: %v", err)
	}

	// Second run without --overwrite must be refused because the output exists.
	if err := conv.runPlannedConversion(plan); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected output-exists error, got: %v", err)
	}

	// With --overwrite the same plan runs again successfully.
	conv.options.Overwrite = true
	if err := conv.runPlannedConversion(plan); err != nil {
		t.Fatalf("overwrite run failed: %v", err)
	}
}

func TestFFmpegFailureSurfacesClearError(t *testing.T) {
	fakeBinDir(t)
	t.Setenv("FAKE_FFMPEG_LOG", filepath.Join(t.TempDir(), "ffmpeg.log"))

	workDir := t.TempDir()
	input := filepath.Join(workDir, "ffmpeg_fail.mp4")
	if err := os.WriteFile(input, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	// Catch-all rule so every file matches a preset.
	cfgPath := filepath.Join(t.TempDir(), "presets.json")
	if err := os.WriteFile(cfgPath, []byte(`{
      "rules": [
        {"name": "all", "match": {}, "preset": {"container": "mp4", "video_codec": "libx264"}}
      ]
    }`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, _ := presets.Load(cfgPath)
	conv := NewConverter(baseOptions(t)).WithPresets(presets.NewEngine(cfg))

	plan, err := conv.buildPlan(input, "", false)
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}
	err = conv.runPlannedConversion(plan)
	if err == nil || !strings.Contains(err.Error(), "ffmpeg conversion failed") {
		t.Fatalf("expected ffmpeg failure error, got: %v", err)
	}
}

func TestLegacyConversionArgsUnchangedWithoutPresets(t *testing.T) {
	fakeBinDir(t)
	logPath := filepath.Join(t.TempDir(), "ffmpeg.log")
	t.Setenv("FAKE_FFMPEG_LOG", logPath)

	workDir := t.TempDir()
	input := filepath.Join(workDir, "vertical_short.mp4")
	if err := os.WriteFile(input, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	opts := baseOptions(t)
	opts.InputPath = input
	conv := NewConverter(opts) // no engine attached

	if err := conv.Convert(); err != nil {
		t.Fatalf("legacy conversion failed: %v", err)
	}

	logged := string(mustReadFile(t, logPath))
	for _, want := range []string{"-c:v libx264", "-c:a aac", "-pix_fmt yuv420p", "-crf 23", "-preset medium"} {
		if !strings.Contains(logged, want) {
			t.Errorf("legacy command missing %q:\n%s", want, logged)
		}
	}
	for _, unwanted := range []string{"-vf", "-noautorotate", " -an", "libvpx-vp9"} {
		if strings.Contains(logged, unwanted) {
			t.Errorf("legacy command must not contain %q:\n%s", unwanted, logged)
		}
	}
	// Same container as the input: legacy naming appends _converted so ffmpeg
	// never writes over its own input.
	lastField := strings.Fields(logged)[len(strings.Fields(logged))-1]
	if !strings.HasSuffix(lastField, "vertical_short_converted.mp4") {
		t.Errorf("legacy output naming changed, logged:\n%s", logged)
	}
}

func TestBatchPresetConversionUsesPerFileContainers(t *testing.T) {
	fakeBinDir(t)
	logPath := filepath.Join(t.TempDir(), "ffmpeg.log")
	t.Setenv("FAKE_FFMPEG_LOG", logPath)

	workDir := t.TempDir()
	for _, name := range []string{"vertical_short.mp4", "landscape_4k.mp4", "silent_clip.mov"} {
		if err := os.WriteFile(filepath.Join(workDir, name), []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
	}
	outputDir := filepath.Join(workDir, "converted")

	opts := baseOptions(t)
	opts.InputPath = workDir
	opts.OutputPath = outputDir
	opts.Concurrent = 1
	conv, _ := newTestConverter(t, opts, true)

	if err := conv.Convert(); err != nil {
		t.Fatalf("batch conversion failed: %v", err)
	}

	for _, expected := range []string{"vertical_short.mp4", "landscape_4k.mkv", "silent_clip.webm"} {
		if _, err := os.Stat(filepath.Join(outputDir, expected)); err != nil {
			t.Errorf("expected batch output %s: %v", expected, err)
		}
	}
	logged := string(mustReadFile(t, logPath))
	// One invocation per file, each recorded as a single argv line.
	if lines := strings.Split(strings.TrimSpace(logged), "\n"); len(lines) != 3 {
		t.Fatalf("expected 3 ffmpeg invocations, got %d:\n%s", len(lines), logged)
	}
	if !strings.Contains(logged, "libvpx-vp9") || !strings.Contains(logged, "libx264") {
		t.Errorf("batch did not use per-rule encoders:\n%s", logged)
	}
}

func TestPreviewReportsDecisionsWithoutConverting(t *testing.T) {
	fakeBinDir(t)
	logPath := filepath.Join(t.TempDir(), "ffmpeg.log")
	t.Setenv("FAKE_FFMPEG_LOG", logPath)

	workDir := t.TempDir()
	for _, name := range []string{"vertical_short.mp4", "landscape_4k.mp4", "silent_clip.mov", "probe_fail.mp4"} {
		if err := os.WriteFile(filepath.Join(workDir, name), []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
	}

	opts := baseOptions(t)
	opts.InputPath = workDir
	conv, _ := newTestConverter(t, opts, true)

	output := captureStdout(t, func() {
		err := conv.Preview()
		if err == nil || !strings.Contains(err.Error(), "1 file(s) could not be previewed") {
			t.Errorf("expected summary error for the failing fixture, got: %v", err)
		}
	})

	for _, want := range []string{
		"vertical-shorts", "4k-landscape", "silent-clips",
		"ffmpeg", "scale=", "probe failed",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("preview output missing %q:\n%s", want, output)
		}
	}
	if _, err := os.Stat(logPath); err == nil {
		t.Error("preview must never invoke ffmpeg")
	}
}

func TestPreviewWithoutPresetsShowsDefaultBehavior(t *testing.T) {
	fakeBinDir(t)
	input := filepath.Join(t.TempDir(), "silent_clip.mov")
	if err := os.WriteFile(input, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	opts := baseOptions(t)
	opts.InputPath = input
	conv := NewConverter(opts)

	output := captureStdout(t, func() {
		if err := conv.Preview(); err != nil {
			t.Errorf("preview failed: %v", err)
		}
	})
	if !strings.Contains(output, "no preset config supplied") {
		t.Errorf("preview should explain default conversion:\n%s", output)
	}
}

// captureStdout runs fn while os.Stdout is redirected to a temp file and
// returns everything it wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	original := os.Stdout
	path := filepath.Join(t.TempDir(), "stdout.txt")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create capture file: %v", err)
	}
	os.Stdout = f
	fn()
	os.Stdout = original
	if err := f.Close(); err != nil {
		t.Fatalf("close capture file: %v", err)
	}
	return string(mustReadFile(t, path))
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
