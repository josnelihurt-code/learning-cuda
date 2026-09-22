package video

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

func generatePreview(ctx context.Context, videoPath, previewPath string) error {
	// TODO: Replace exec.Command with a Go library (consider github.com/u2takey/ffmpeg-go or github.com/giorgisio/goav)
	// to avoid external process dependency and improve error handling
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		videoPath,
	)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("ffprobe failed: %w", err)
	}

	duration, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil {
		return fmt.Errorf("invalid duration: %w", err)
	}

	timestamp := 5.0
	if duration < timestamp {
		timestamp = duration * 0.5
	}

	log.Debug().
		Str("video_path", videoPath).
		Float64("duration", duration).
		Float64("timestamp", timestamp).
		Msg("Generating video preview")

	// #nosec G204 -- videoPath and previewPath are validated by caller
	cmd = exec.CommandContext(ctx, "ffmpeg",
		"-ss", fmt.Sprintf("%.2f", timestamp),
		"-i", videoPath,
		"-vframes", "1",
		"-q:v", "2",
		"-y",
		previewPath,
	)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg preview generation failed: %w", err)
	}

	log.Info().
		Str("video_path", videoPath).
		Str("preview_path", previewPath).
		Float64("timestamp", timestamp).
		Msg("Video preview generated successfully")

	return nil
}
