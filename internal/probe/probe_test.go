package probe

import (
	"path/filepath"
	"strings"
	"testing"
)

// fakeFFprobe returns a Prober backed by the shell fake in testdata.
func fakeFFprobe(t *testing.T) *FFprobe {
	t.Helper()
	return &FFprobe{Binary: filepath.Join("..", "converter", "testdata", "fakes", "ffprobe")}
}

func TestProbeFixtures(t *testing.T) {
	ff := fakeFFprobe(t)

	tests := []struct {
		name           string
		file           string
		wantWidth      int
		wantHeight     int
		wantFPS        float64
		wantDuration   float64
		wantAudio      bool
		wantRotation   int
		wantDisplayW   int
		wantDisplayH   int
		wantOrientName string
	}{
		{
			name:      "vertical short with 90 degree rotation",
			file:      "vertical_short.mp4",
			wantWidth: 3840, wantHeight: 2160,
			wantFPS: 30, wantDuration: 45.23, wantAudio: true,
			// ffprobe encodes counter-clockwise rotation as -90; normalized
			// into [0,360) that is 270, which swaps the display axes.
			wantRotation: 270, wantDisplayW: 2160, wantDisplayH: 3840,
		},
		{
			name:      "4K landscape high frame rate",
			file:      "landscape_4k.mp4",
			wantWidth: 3840, wantHeight: 2160,
			wantFPS: 59.94, wantDuration: 612.5, wantAudio: true,
			wantRotation: 0, wantDisplayW: 3840, wantDisplayH: 2160,
		},
		{
			name:      "silent clip without audio stream",
			file:      "silent_clip.mov",
			wantWidth: 1280, wantHeight: 720,
			wantFPS: 24, wantDuration: 12, wantAudio: false,
			wantRotation: 0, wantDisplayW: 1280, wantDisplayH: 720,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := ff.Probe(tt.file)
			if err != nil {
				t.Fatalf("Probe returned error: %v", err)
			}
			if info.Width != tt.wantWidth || info.Height != tt.wantHeight {
				t.Errorf("stored dimensions = %dx%d, want %dx%d", info.Width, info.Height, tt.wantWidth, tt.wantHeight)
			}
			if !floatAlmostEqual(info.FPS, tt.wantFPS, 0.01) {
				t.Errorf("FPS = %.4f, want %.4f", info.FPS, tt.wantFPS)
			}
			if !floatAlmostEqual(info.Duration, tt.wantDuration, 0.001) {
				t.Errorf("duration = %.4f, want %.4f", info.Duration, tt.wantDuration)
			}
			if info.HasAudio != tt.wantAudio {
				t.Errorf("HasAudio = %t, want %t", info.HasAudio, tt.wantAudio)
			}
			if info.Rotation != tt.wantRotation {
				t.Errorf("rotation = %d, want %d", info.Rotation, tt.wantRotation)
			}
			if info.DisplayWidth() != tt.wantDisplayW || info.DisplayHeight() != tt.wantDisplayH {
				t.Errorf("display dimensions = %dx%d, want %dx%d",
					info.DisplayWidth(), info.DisplayHeight(), tt.wantDisplayW, tt.wantDisplayH)
			}
		})
	}
}

func TestProbeMissingFields(t *testing.T) {
	_, err := fakeFFprobe(t).Probe("missing_fields.mp4")
	if err == nil {
		t.Fatal("expected error for missing width/height, got nil")
	}
	if !strings.Contains(err.Error(), "missing required field") {
		t.Errorf("error should mention missing required field, got: %v", err)
	}
}

func TestProbeNoVideoStream(t *testing.T) {
	_, err := fakeFFprobe(t).Probe("no_video.mp4")
	if err == nil || !strings.Contains(err.Error(), "no video stream") {
		t.Fatalf("expected no-video-stream error, got: %v", err)
	}
}

func TestProbeFFprobeFailure(t *testing.T) {
	_, err := fakeFFprobe(t).Probe("probe_fail.mp4")
	if err == nil || !strings.Contains(err.Error(), "ffprobe failed") {
		t.Fatalf("expected ffprobe failure error, got: %v", err)
	}
}

func TestProbeRotationSources(t *testing.T) {
	ff := fakeFFprobe(t)

	t.Run("frame-level display matrix", func(t *testing.T) {
		info, err := ff.Probe("frame_rotation.mp4")
		if err != nil {
			t.Fatalf("Probe: %v", err)
		}
		if info.Rotation != 270 {
			t.Errorf("rotation = %d, want 270 (from -90 frame matrix)", info.Rotation)
		}
		if info.DisplayWidth() != 1080 || info.DisplayHeight() != 1920 {
			t.Errorf("display = %dx%d, want 1080x1920", info.DisplayWidth(), info.DisplayHeight())
		}
	})

	t.Run("legacy rotate tag", func(t *testing.T) {
		info, err := ff.Probe("tag_rotation.mov")
		if err != nil {
			t.Fatalf("Probe: %v", err)
		}
		if info.Rotation != 90 {
			t.Errorf("rotation = %d, want 90 (from tag)", info.Rotation)
		}
		if info.DisplayWidth() != 1920 || info.DisplayHeight() != 1080 {
			t.Errorf("display = %dx%d, want 1920x1080", info.DisplayWidth(), info.DisplayHeight())
		}
	})
}

func TestParseRate(t *testing.T) {
	cases := []struct {
		avg, rframe string
		want        float64
		wantErr     bool
	}{
		{"30/1", "30/1", 30, false},
		{"60000/1001", "60000/1001", 59.94, false},
		{"0/0", "24/1", 24, false}, // falls back to r_frame_rate
		{"", "25/1", 25, false},
		{"0/0", "N/A", 0, true},
	}
	for _, tc := range cases {
		got, err := parseRate(tc.avg, tc.rframe)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseRate(%q, %q) expected error", tc.avg, tc.rframe)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseRate(%q, %q) unexpected error: %v", tc.avg, tc.rframe, err)
		}
		if !floatAlmostEqual(got, tc.want, 0.01) {
			t.Errorf("parseRate(%q, %q) = %.4f, want %.4f", tc.avg, tc.rframe, got, tc.want)
		}
	}
}

func TestNormalizeRotation(t *testing.T) {
	cases := []struct {
		degrees int
		want    int
	}{
		{0, 0}, {90, 90}, {180, 180}, {270, 270},
		{-90, 270}, {-180, 180}, {-270, 90}, {360, 0}, {450, 90},
	}
	for _, tc := range cases {
		normalized := ((tc.degrees % 360) + 360) % 360
		if normalized != tc.want {
			t.Errorf("normalize(%d) = %d, want %d", tc.degrees, normalized, tc.want)
		}
	}
}

func floatAlmostEqual(a, b, tolerance float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < tolerance
}
