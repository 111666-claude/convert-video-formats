package converter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vib795/convert-video-formats/internal/presets"
	"github.com/vib795/convert-video-formats/internal/probe"
	"github.com/vib795/convert-video-formats/internal/progress"
	"github.com/vib795/convert-video-formats/internal/utils"
	"github.com/vib795/convert-video-formats/pkg/types"
)

// transcodePlan is the fully resolved per-file conversion: the rule decision,
// the output path and the exact ffmpeg arguments to run.
type transcodePlan struct {
	inputPath  string
	outputPath string
	info       probe.MediaInfo
	decision   presets.Decision
	args       []string
}

// buildPlan probes inputPath, evaluates the preset rules and, when a rule
// matches, resolves the output path and ffmpeg arguments. A nil plan with no
// error means no rule matched (or no engine is configured), so the caller
// falls back to the original fixed-parameter conversion.
// outputIsDir tells the resolver whether explicitOutput names a directory
// (batch mode), since it may not have been created yet at planning time.
func (c *Converter) buildPlan(inputPath, explicitOutput string, outputIsDir bool) (*transcodePlan, error) {
	if c.engine == nil {
		return nil, nil
	}

	info, err := c.prober.Probe(inputPath)
	if err != nil {
		return nil, fmt.Errorf("cannot determine transcoding preset for %s: %w", filepath.Base(inputPath), err)
	}

	decision := c.engine.Decide(info)
	if !decision.Matched {
		return nil, nil
	}

	outputPath, err := c.planOutputPath(inputPath, explicitOutput, decision.Preset.Container, outputIsDir)
	if err != nil {
		return nil, err
	}

	args := c.buildPresetArgs(inputPath, outputPath, decision)
	return &transcodePlan{
		inputPath:  inputPath,
		outputPath: outputPath,
		info:       info,
		decision:   decision,
		args:       args,
	}, nil
}

// planOutputPath derives the output path for a matched rule. explicitOutput
// may be empty (auto naming), an existing directory, or an explicit file
// path which is honored verbatim. The extension always follows the rule's
// target container.
func (c *Converter) planOutputPath(inputPath, explicitOutput, container string, outputIsDir bool) (string, error) {
	baseName := filepath.Base(inputPath)
	ext := filepath.Ext(baseName)
	nameWithoutExt := strings.TrimSuffix(baseName, ext)

	if explicitOutput != "" {
		if outputIsDir {
			if err := os.MkdirAll(explicitOutput, 0755); err != nil {
				return "", fmt.Errorf("failed to create output directory: %w", err)
			}
			return filepath.Join(explicitOutput, fmt.Sprintf("%s.%s", nameWithoutExt, container)), nil
		}
		if info, err := os.Stat(explicitOutput); err == nil && info.IsDir() {
			return filepath.Join(explicitOutput, fmt.Sprintf("%s.%s", nameWithoutExt, container)), nil
		}
		// Explicit file path: keep it exactly as the user requested.
		return explicitOutput, nil
	}

	// Auto naming mirrors utils.GetOutputPath, including the same-name guard.
	outputPath := fmt.Sprintf("%s.%s", strings.TrimSuffix(inputPath, ext), container)
	if strings.EqualFold(outputPath, inputPath) {
		outputPath = fmt.Sprintf("%s_converted.%s", strings.TrimSuffix(inputPath, ext), container)
	}
	return outputPath, nil
}

// buildPresetArgs constructs the ffmpeg arguments for a matched rule.
//
// Rotation metadata is preserved by passing -noautorotate so ffmpeg neither
// rotates the frames nor drops the display matrix. Scale filters therefore
// operate on coded (stored) dimensions, which are swapped relative to the
// display dimensions for 90/270 degree rotation.
func (c *Converter) buildPresetArgs(inputPath, outputPath string, decision presets.Decision) []string {
	args := []string{"-nostdin"}
	if decision.Info.Rotation != 0 {
		args = append(args, "-noautorotate")
	}
	args = append(args,
		"-i", inputPath,
		"-progress", "pipe:1",
		"-nostats",
	)

	args = appendPresetCodecArgs(args, decision)

	if decision.Info.HasAudio {
		args = append(args, "-c:a", decision.EffectiveAudioCodec())
	} else {
		// Silent source: do not emit an empty audio track.
		args = append(args, "-an")
	}

	if !decision.Scale.NoScale {
		filterW, filterH := decision.Scale.Width, decision.Scale.Height
		if decision.Info.Rotation == 90 || decision.Info.Rotation == 270 {
			filterW, filterH = decision.Scale.Height, decision.Scale.Width
		}
		args = append(args, "-vf", fmt.Sprintf("scale=%d:%d:flags=lanczos", filterW, filterH))
	}

	if c.options.Overwrite {
		args = append(args, "-y")
	} else {
		args = append(args, "-n")
	}
	args = append(args, outputPath)
	return args
}

// appendPresetCodecArgs adds encoder selection and rate-control settings for
// the rule's video codec, reusing the same quality tiers as buildFFmpegArgs.
func appendPresetCodecArgs(args []string, decision presets.Decision) []string {
	quality := strings.ToLower(decision.Preset.Quality)

	switch decision.Preset.VideoCodec {
	case "libvpx-vp9":
		args = append(args, "-c:v", "libvpx-vp9")
		switch quality {
		case "high":
			args = append(args, "-crf", "15", "-b:v", "0")
		case "low":
			args = append(args, "-crf", "40", "-b:v", "0")
		default:
			args = append(args, "-crf", "30", "-b:v", "0")
		}
	default: // libx264 and any future AVC-family encoder
		args = append(args, "-c:v", "libx264", "-pix_fmt", "yuv420p")
		switch quality {
		case "high":
			args = append(args, "-crf", "18", "-preset", "slow")
		case "low":
			args = append(args, "-crf", "28", "-preset", "fast")
		default:
			args = append(args, "-crf", "23", "-preset", "medium")
		}
	}
	return args
}

// runPlannedConversion executes a plan for a single file, reusing the
// existing progress-bar machinery. It mirrors convertSingleFile's success
// and error reporting.
func (c *Converter) runPlannedConversion(plan *transcodePlan) error {
	if utils.FileExists(plan.outputPath) && !c.options.Overwrite {
		return fmt.Errorf("output file %s already exists. Use --overwrite to replace it", plan.outputPath)
	}
	if err := utils.EnsureDirectoryExists(filepath.Dir(plan.outputPath)); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	bar := progress.NewProgressBar(fmt.Sprintf("Converting %s", filepath.Base(plan.inputPath)))
	bar.Start()

	cmd, encoded, err := c.startFFmpegWithArgs(plan.args)
	if err != nil {
		bar.Stop()
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	stopProgress := make(chan bool)
	go c.monitorOutputProgress(plan.outputPath, bar, plan.info.Duration, encoded, stopProgress)

	if err := cmd.Wait(); err != nil {
		close(stopProgress)
		bar.Error(fmt.Sprintf("Failed to convert %s", filepath.Base(plan.inputPath)))
		return wrapFFmpegError(plan.inputPath, plan.outputPath, err, c.options.Overwrite)
	}

	close(stopProgress)
	bar.Success(fmt.Sprintf("Converted %s → %s", filepath.Base(plan.inputPath), filepath.Base(plan.outputPath)))
	return nil
}

// runPlannedConversionBatch executes a plan inside a batch worker, reporting
// into the shared batch progress display.
func (c *Converter) runPlannedConversionBatch(plan *transcodePlan, baseName string, batchProgress *progress.BatchProgress) types.ConversionResult {
	result := types.ConversionResult{
		InputFile:  plan.inputPath,
		OutputFile: plan.outputPath,
		Success:    false,
	}

	if utils.FileExists(plan.outputPath) && !c.options.Overwrite {
		result.Error = fmt.Errorf("output file already exists")
		batchProgress.CompleteFile(baseName, false)
		return result
	}

	cmd, encoded, err := c.startFFmpegWithArgs(plan.args)
	if err != nil {
		result.Error = fmt.Errorf("failed to start ffmpeg: %w", err)
		batchProgress.CompleteFile(baseName, false)
		return result
	}

	stopProgress := make(chan bool)
	go c.monitorBatchProgress(plan.outputPath, batchProgress, baseName, plan.info.Duration, encoded, stopProgress)

	if err := cmd.Wait(); err != nil {
		close(stopProgress)
		result.Error = err
		batchProgress.CompleteFile(baseName, false)
		return result
	}

	close(stopProgress)
	result.Success = true
	batchProgress.CompleteFile(baseName, true)
	return result
}

// wrapFFmpegError produces the same guidance as the original conversion path.
func wrapFFmpegError(inputPath, outputPath string, runErr error, overwrite bool) error {
	if utils.FileExists(outputPath) && !overwrite {
		return fmt.Errorf("conversion failed: output file already exists. Use --overwrite to replace it")
	}
	inputCodec := utils.GetInputVideoCodec(inputPath)
	if inputCodec == "av1" && !utils.HasAV1DecoderSupport() {
		return fmt.Errorf("ffmpeg conversion failed: %w\n\n%s", runErr, utils.GetAV1ErrorMessage())
	}
	return fmt.Errorf("ffmpeg conversion failed: %w. Ensure ffmpeg is properly installed and the input file is valid", runErr)
}
