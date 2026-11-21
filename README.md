# convert-vid

A fast and efficient command-line tool for converting video files between different formats. Built with Go and powered by FFmpeg.

## Features

- **Single File Conversion**: Convert individual video files
- **Batch Conversion**: Convert entire directories of videos concurrently
- **Multiple Formats**: Support for mp4, avi, mov, mkv, webm, flv
- **Quality Presets**: Choose between high, medium, and low quality settings
- **Conditional Transcoding Presets**: Match ordered rules against dimensions, frame rate, duration and audio track to pick encoder, scaling cap and container per file, with a dry-run preview
- **Progress Tracking**: Visual feedback during conversion
- **Concurrent Processing**: Multi-threaded batch conversions for faster processing
- **Simple CLI**: Easy-to-use command-line interface

## Demo

![convert-vid demo](demo.gif)

## Prerequisites

**FFmpeg** must be installed on your system:

### macOS
```bash
brew install ffmpeg
```

### Ubuntu/Debian
```bash
sudo apt update
sudo apt install ffmpeg
```

### Windows
Download from [ffmpeg.org](https://ffmpeg.org/download.html) or use chocolatey:
```bash
choco install ffmpeg
```

### Verify Installation
```bash
ffmpeg -version
```

## Installation

### From Source

1. Clone the repository:
```bash
git clone https://github.com/vib795/convert-video-formats.git
cd convert-video-formats
```

2. Build the binary:
```bash
go build -o convert-vid
```

3. (Optional) Move to your PATH:
```bash
# macOS/Linux
sudo mv convert-vid /usr/local/bin/

# Or add to your PATH
export PATH=$PATH:$(pwd)
```

### Using Go Install

```bash
go install github.com/vib795/convert-video-formats@latest
```

## Usage

### Basic Commands

```bash
# Convert a single file to mp4
convert-vid convert input.avi --format mp4 --output output.mp4

# Convert using short flags
convert-vid convert input.mov -f mp4 -o output.mp4

# Convert with automatic output naming
convert-vid convert input.avi -f mp4

# Convert all videos in a folder
convert-vid convert ./videos --format mp4 --output ./converted

# Convert with high quality
convert-vid convert input.mov -f mp4 -q high

# Overwrite existing files
convert-vid convert input.avi -f mp4 --overwrite

# Specify concurrent conversions for batch processing
convert-vid convert ./videos -f mp4 -c 4
```

### Handling Paths with Spaces

**Important**: When working with file or folder paths that contain spaces, you must wrap them in quotes:

```bash
# Method 1: Use quotes with positional argument (recommended for simple cases)
convert-vid convert "My Video.mov" -f mp4 -o "Output Video.mp4"
convert-vid convert "~/Downloads/My Videos/lecture.webm" -f mp4

# Method 2: Use --input flag (recommended for complex paths)
convert-vid convert --input "~/Downloads/My Videos/lecture.webm" -f mp4 -o "output.mp4"
convert-vid convert -i "Backend from first principles/01 - Roadmap.webm" -f mp4

# Method 3: For directories with spaces
convert-vid convert --input "~/Downloads/My Video Collection" -f mp4 -o "./Converted Videos"

# Real-world example
convert-vid convert \
  --input "~/Downloads/pull-vids/Backend from first principles/01 - 1. Roadmap for backend from first principles.webm" \
  --format mp4 \
  --output "1. Roadmap for backend from first principles.mp4"
```

**Tips**:
- Always use **double quotes** (`"`) around paths with spaces
- The `--input` or `-i` flag is often clearer than positional arguments for complex paths
- You can use `\` at the end of a line to continue a long command on the next line

### Command Reference

```bash
convert-vid convert [input] [flags]
```

#### Flags

| Flag | Short | Description | Default |
|------|-------|-------------|---------|
| `--input` | `-i` | Input video file or directory path (recommended for paths with spaces) | - |
| `--format` | `-f` | Target video format (mp4, avi, mov, mkv, webm, flv) | mp4 |
| `--output` | `-o` | Output file or directory path (use quotes for paths with spaces) | Auto-generated |
| `--quality` | `-q` | Quality preset (high, medium, low) | medium |
| `--concurrent` | `-c` | Number of concurrent conversions for batch | CPU count |
| `--presets` | `-p` | Conditional transcoding preset config (JSON) | none |
| `--preview` | | Print per-file probe data, rule decisions and ffmpeg commands without converting | false |
| `--overwrite` | | Overwrite existing output files | false |
| `--help` | `-h` | Show help | |

**Note**: You can provide the input path either as a positional argument (`convert-vid convert "path"`) or using the `--input` flag (`convert-vid convert --input "path"`). The flag method is recommended for paths containing spaces.

#### Quality Presets

- **high**: CRF 18, slow preset (best quality, larger files)
- **medium**: CRF 23, medium preset (balanced)
- **low**: CRF 28, fast preset (smaller files, lower quality)

### Examples

#### Convert a Single File

```bash
# Basic conversion
convert-vid convert vacation.avi -f mp4

# With custom output and high quality
convert-vid convert vacation.avi -f mp4 -o vacation_hd.mp4 -q high

# Convert to different formats
convert-vid convert video.mp4 -f webm
convert-vid convert video.avi -f mkv
convert-vid convert video.mov -f flv
```

#### Batch Convert a Directory

```bash
# Convert all videos in ./videos to mp4
convert-vid convert ./videos -f mp4

# Convert with custom output directory
convert-vid convert ./videos -f mp4 -o ./converted_videos

# Convert with 4 concurrent workers
convert-vid convert ./videos -f mp4 -c 4 -o ./output

# Convert with high quality and overwrite
convert-vid convert ./videos -f mp4 -q high --overwrite
```

## Conditional Transcoding Presets

A single parameter set wastes space on short vertical clips and under-compresses 4K landscape footage. A preset config (JSON) lets each file pick its own encoder, scaling cap and target container from ordered rules matched against ffprobe metadata.

### Previewing decisions first

`--preview` probes every input and prints the decision trail without invoking ffmpeg:

```bash
convert-vid convert ./videos -f mp4 -p presets.json --preview
```

```
== vertical_short.mp4 ==
probe: 3840x2160 stored, 2160x3840 display (rotation 270), 30.000 fps, 45.23s, audio=true
  rule "vertical-shorts": MATCH
decision: rule "vertical-shorts" -> container=mp4 video=libx264 audio=aac quality=medium
scale: 2160x3840 -> 1080x1920 (aspect ratio preserved, only downscaling)
output: videos/vertical_short.mp4
ffmpeg -nostdin -noautorotate -i ... -c:v libx264 -c:a aac -vf scale=1920:1080:flags=lanczos -n ...
```

Rules are evaluated **in file order; the first match wins**, and every skipped rule records why. A file no rule matches falls back to the normal `--format`/`--quality` conversion, so a partial config is safe. Without `--presets` the tool behaves exactly as before.

### Config format

A complete example ships in [`examples/presets.json`](examples/presets.json).

```json
{
  "rules": [
    {
      "name": "vertical-shorts",
      "match": {
        "orientation": "portrait",
        "max_duration": 120,
        "has_audio": true
      },
      "preset": {
        "container": "mp4",
        "video_codec": "libx264",
        "max_scale_width": 1080,
        "max_scale_height": 1920,
        "quality": "medium"
      }
    }
  ]
}
```

**Match conditions** — all present conditions must hold; omitted ones impose no constraint. Dimensions are compared against the *display* dimensions (after rotation):

| Field | Meaning |
|---|---|
| `orientation` | `portrait`, `landscape` or `square` |
| `min_width` / `max_width` | display width bounds in pixels |
| `min_height` / `max_height` | display height bounds in pixels |
| `min_fps` / `max_fps` | average frame-rate bounds |
| `min_duration` / `max_duration` | duration bounds in seconds |
| `has_audio` | `true` requires an audio stream, `false` requires none |

**Preset actions**:

| Field | Meaning |
|---|---|
| `container` | target container: `mp4`, `avi`, `mov`, `mkv`, `webm`, `flv` |
| `video_codec` | `libx264` or `libvpx-vp9` |
| `audio_codec` | optional; defaults to `aac` (`libopus` for WebM). Silent sources get `-an` instead |
| `max_scale_width` / `max_scale_height` | optional even-pixel caps; sources are only downscaled, never upscaled |
| `quality` | `high`, `medium` (default) or `low` |

### Aspect ratio and rotation

- Scaling preserves aspect ratio and rounds to even pixels (required by H.264/YUV 4:2:0).
- Rotation metadata is never baked or dropped: rotated inputs are probed by display dimensions, scaled on the correct swapped axes, and passed `-noautorotate` so the display matrix is copied to the output.
- A rule whose container matches the input extension auto-names the output `name_converted.ext`, mirroring the built-in same-name guard.

### Validation errors

Config loading fails fast, before any file is touched, on: duplicate or missing rule names, unknown JSON fields, inverted bounds, unsupported containers/codecs/quality values, and scale caps that are negative, odd or below 2. Other explicit outcomes:

- **Missing probe fields** (no video stream, no dimensions/frame rate/duration): the file is reported with a `probe data missing required field` error and skipped in batch runs.
- **ffprobe/ffmpeg failure**: surfaced as `ffprobe failed` / `ffmpeg conversion failed` with the underlying cause.
- **Output already exists**: refused with an `already exists` message unless `--overwrite` is given (also flagged in `--preview`).

## Supported Formats

### Input Formats
The tool can read most common video formats including:
- mp4, avi, mov, mkv, webm, flv
- m4v, mpg, mpeg, wmv
- And many more supported by FFmpeg

### Output Formats
- **mp4** - MPEG-4 Part 14 (H.264)
- **avi** - Audio Video Interleave
- **mov** - QuickTime File Format
- **mkv** - Matroska Video
- **webm** - WebM
- **flv** - Flash Video

## How It Works

1. **FFmpeg Wrapper**: Uses FFmpeg for the actual video conversion
2. **Concurrent Processing**: Batch conversions use Go routines for parallel processing
3. **Progress Feedback**: Spinner animations show conversion progress
4. **Smart Defaults**: Automatically generates output filenames and uses optimal settings

## Performance

- **Single File**: Conversion speed depends on video size and quality settings
- **Batch Processing**: Uses worker pool pattern with concurrent processing
- **Default Workers**: Matches your CPU core count for optimal performance
- **Custom Concurrency**: Adjust with `-c` flag based on your system resources

## Project Structure

```
convert-vid-format/
├── main.go                 # Entry point
├── cmd/
│   ├── root.go            # Root command definition
│   └── convert.go         # Convert command implementation
├── internal/
│   ├── converter/
│   │   ├── converter.go   # Core conversion logic
│   │   └── preset_plan.go # Conditional preset planning and ffmpeg args
│   ├── presets/
│   │   └── presets.go     # Ordered rule config, validation and matching
│   ├── probe/
│   │   └── probe.go       # ffprobe metadata wrapper
│   ├── progress/
│   │   └── progress.go    # Progress indicators
│   └── utils/
│       └── utils.go       # Helper functions
└── pkg/
    └── types/
        └── types.go       # Type definitions
```

## Development

### Build

```bash
go build -o convert-vid
```

### Run Tests

```bash
go test ./...
```

### Run Without Building

```bash
go run main.go convert [args]
```

## Troubleshooting

### FFmpeg Not Found
```
Error: ffmpeg is not installed or not in PATH
```
**Solution**: Install FFmpeg (see Prerequisites section)

### Permission Denied
```
Error: permission denied
```
**Solution**: Check file permissions or use `sudo` if needed

### Output File Exists
```
Error: output file already exists
```
**Solution**: Use `--overwrite` flag to replace existing files

### Unsupported Format
```
Error: unsupported format
```
**Solution**: Check that the format is one of: mp4, avi, mov, mkv, webm, flv

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT License - see LICENSE file for details

## Credits

- Built with [Cobra](https://github.com/spf13/cobra) for CLI framework
- Powered by [FFmpeg](https://ffmpeg.org/) for video conversion

## Version

Current version: 1.0.0

---

**Note**: This tool requires FFmpeg to be installed on your system. Make sure FFmpeg is in your PATH before using convert-vid.