package types

// ConversionOptions holds all the options for video conversion
type ConversionOptions struct {
	InputPath   string
	OutputPath  string
	Format      string
	Quality     string
	Overwrite   bool
	Concurrent  int
	IsDirectory bool
	// PresetsPath is the path to a conditional transcoding preset config.
	// When empty, conversion uses the original fixed-parameter behavior.
	PresetsPath string
	// Preview only reports per-file probe results, rule decisions and the
	// ffmpeg commands that would run, without converting anything.
	Preview bool
}

// VideoFormat represents supported video formats
type VideoFormat string

const (
	FormatMP4  VideoFormat = "mp4"
	FormatAVI  VideoFormat = "avi"
	FormatMOV  VideoFormat = "mov"
	FormatMKV  VideoFormat = "mkv"
	FormatWEBM VideoFormat = "webm"
	FormatFLV  VideoFormat = "flv"
)

// Quality represents video quality presets
type Quality string

const (
	QualityHigh   Quality = "high"
	QualityMedium Quality = "medium"
	QualityLow    Quality = "low"
)

// ConversionResult holds the result of a conversion operation
type ConversionResult struct {
	InputFile  string
	OutputFile string
	Success    bool
	Error      error
}
