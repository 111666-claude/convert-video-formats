package presets

import (
	"fmt"
	"strings"

	"github.com/vib795/convert-video-formats/internal/probe"
)

// RuleEvaluation records how one rule fared against a file, making every
// decision explainable.
type RuleEvaluation struct {
	RuleName string
	Matched  bool
	// Reasons holds one human-readable sentence per failed condition when
	// Matched is false; it is empty for a matching rule.
	Reasons []string
}

// Scale describes a resolved downscale. When NoScale is true the source
// dimensions are kept as-is.
type Scale struct {
	NoScale bool
	Width   int
	Height  int
}

// Decision is the result of evaluating all configured rules for one file.
type Decision struct {
	// Matched is false when no rule applied; the caller must then use the
	// original fixed-parameter conversion.
	Matched bool
	// RuleName is the winning rule, or "" when none matched.
	RuleName    string
	Preset      Preset
	Info        probe.MediaInfo
	Scale       Scale
	Evaluations []RuleEvaluation
}

// Explain renders a deterministic, human-readable summary of the decision,
// including every rule that was evaluated and the conditions that failed.
func (d Decision) Explain() string {
	var b strings.Builder
	info := d.Info
	fmt.Fprintf(&b, "probe: %dx%d stored, %dx%d display (rotation %d), %.3f fps, %.2fs, audio=%t\n",
		info.Width, info.Height, info.DisplayWidth(), info.DisplayHeight(),
		info.Rotation, info.FPS, info.Duration, info.HasAudio)

	for _, eval := range d.Evaluations {
		if eval.Matched {
			fmt.Fprintf(&b, "  rule %q: MATCH\n", eval.RuleName)
		} else {
			fmt.Fprintf(&b, "  rule %q: skipped (%s)\n", eval.RuleName, strings.Join(eval.Reasons, "; "))
		}
	}

	if !d.Matched {
		b.WriteString("decision: no rule matched, using default conversion settings\n")
		return b.String()
	}

	fmt.Fprintf(&b, "decision: rule %q -> container=%s video=%s audio=%s quality=%s\n",
		d.RuleName, d.Preset.Container, d.Preset.VideoCodec, d.effectiveAudioCodec(), d.Preset.Quality)
	if d.Scale.NoScale {
		b.WriteString("scale: none (source already within caps)\n")
	} else {
		fmt.Fprintf(&b, "scale: %dx%d -> %dx%d (aspect ratio preserved, only downscaling)\n",
			info.DisplayWidth(), info.DisplayHeight(), d.Scale.Width, d.Scale.Height)
	}
	return b.String()
}

// effectiveAudioCodec resolves the default audio encoder for a preset.
func (d Decision) effectiveAudioCodec() string {
	if d.Preset.AudioCodec != "" {
		return d.Preset.AudioCodec
	}
	if d.Preset.Container == "webm" {
		return "libopus"
	}
	return "aac"
}

// EffectiveAudioCodec is exported for the converter's command builder.
func (d Decision) EffectiveAudioCodec() string {
	return d.effectiveAudioCodec()
}

// Engine evaluates an ordered list of rules against probed media info.
type Engine struct {
	rules []Rule
}

// NewEngine builds an Engine from a validated config.
func NewEngine(cfg *Config) *Engine {
	rules := []Rule{}
	if cfg != nil {
		rules = cfg.Rules
	}
	return &Engine{rules: rules}
}

// Decide runs the rules in order and returns the first match, together with
// the full evaluation trail. With no rules configured the decision never
// matches, which leaves the original conversion behavior untouched.
func (e *Engine) Decide(info probe.MediaInfo) Decision {
	decision := Decision{Info: info, Evaluations: []RuleEvaluation{}}

	for _, rule := range e.rules {
		eval := evaluateRule(rule, info)
		decision.Evaluations = append(decision.Evaluations, eval)
		if eval.Matched {
			decision.Matched = true
			decision.RuleName = rule.Name
			decision.Preset = rule.Preset
			decision.Scale = resolveScale(info.DisplayWidth(), info.DisplayHeight(), rule.Preset)
			return decision
		}
	}
	return decision
}

// evaluateRule checks every condition of a rule; all must hold for a match.
func evaluateRule(rule Rule, info probe.MediaInfo) RuleEvaluation {
	eval := RuleEvaluation{RuleName: rule.Name, Matched: true}
	m := rule.Match
	displayW, displayH := info.DisplayWidth(), info.DisplayHeight()

	if m.Orientation != "" {
		want := strings.ToLower(m.Orientation)
		got := orientation(displayW, displayH)
		if got != want {
			eval.Matched = false
			eval.Reasons = append(eval.Reasons, fmt.Sprintf("orientation is %s, want %s", got, want))
		}
	}
	if m.MinWidth > 0 && displayW < m.MinWidth {
		eval.Matched = false
		eval.Reasons = append(eval.Reasons, fmt.Sprintf("display width %d < min_width %d", displayW, m.MinWidth))
	}
	if m.MaxWidth > 0 && displayW > m.MaxWidth {
		eval.Matched = false
		eval.Reasons = append(eval.Reasons, fmt.Sprintf("display width %d > max_width %d", displayW, m.MaxWidth))
	}
	if m.MinHeight > 0 && displayH < m.MinHeight {
		eval.Matched = false
		eval.Reasons = append(eval.Reasons, fmt.Sprintf("display height %d < min_height %d", displayH, m.MinHeight))
	}
	if m.MaxHeight > 0 && displayH > m.MaxHeight {
		eval.Matched = false
		eval.Reasons = append(eval.Reasons, fmt.Sprintf("display height %d > max_height %d", displayH, m.MaxHeight))
	}
	if m.MinFPS > 0 && info.FPS < m.MinFPS {
		eval.Matched = false
		eval.Reasons = append(eval.Reasons, fmt.Sprintf("frame rate %.3f < min_fps %g", info.FPS, m.MinFPS))
	}
	if m.MaxFPS > 0 && info.FPS > m.MaxFPS {
		eval.Matched = false
		eval.Reasons = append(eval.Reasons, fmt.Sprintf("frame rate %.3f > max_fps %g", info.FPS, m.MaxFPS))
	}
	if m.MinDuration > 0 && info.Duration < m.MinDuration {
		eval.Matched = false
		eval.Reasons = append(eval.Reasons, fmt.Sprintf("duration %.2fs < min_duration %g", info.Duration, m.MinDuration))
	}
	if m.MaxDuration > 0 && info.Duration > m.MaxDuration {
		eval.Matched = false
		eval.Reasons = append(eval.Reasons, fmt.Sprintf("duration %.2fs > max_duration %g", info.Duration, m.MaxDuration))
	}
	if m.HasAudio != nil && info.HasAudio != *m.HasAudio {
		eval.Matched = false
		eval.Reasons = append(eval.Reasons, fmt.Sprintf("has_audio is %t, want %t", info.HasAudio, *m.HasAudio))
	}
	return eval
}

// orientation classifies display dimensions.
func orientation(width, height int) string {
	switch {
	case width > height:
		return "landscape"
	case width < height:
		return "portrait"
	default:
		return "square"
	}
}

// resolveScale computes the target dimensions. The source is never upscaled;
// both caps shrink the same dimension ratio and the result is rounded to even
// pixels (required by H.264/YUV420p).
func resolveScale(srcWidth, srcHeight int, p Preset) Scale {
	scaleW, scaleH := srcWidth, srcHeight

	if p.MaxScaleWidth > 0 && scaleW > p.MaxScaleWidth {
		scaleW = p.MaxScaleWidth
		scaleH = evenRound(float64(srcHeight) * float64(p.MaxScaleWidth) / float64(srcWidth))
	}
	if p.MaxScaleHeight > 0 && scaleH > p.MaxScaleHeight {
		scaleH = p.MaxScaleHeight
		scaleW = evenRound(float64(scaleW) * float64(p.MaxScaleHeight) / float64(scaleH))
	}
	// Rounding can push the width back over its cap by a pixel.
	if p.MaxScaleWidth > 0 && scaleW > p.MaxScaleWidth {
		scaleW = p.MaxScaleWidth
	}

	if scaleW == srcWidth && scaleH == srcHeight {
		return Scale{NoScale: true}
	}
	return Scale{Width: scaleW, Height: scaleH}
}

// evenRound rounds to the nearest even integer, with a floor of 2.
func evenRound(value float64) int {
	rounded := int(value + 0.5)
	if rounded < 2 {
		return 2
	}
	if rounded%2 != 0 {
		rounded--
	}
	return rounded
}
