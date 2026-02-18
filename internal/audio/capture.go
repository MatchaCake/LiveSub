package audio

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
)

// Capturer captures audio from a live stream URL via ffmpeg.
type Capturer struct {
	SampleRate int
	Channels   int
}

func NewCapturer() *Capturer {
	return &Capturer{
		SampleRate: 16000,
		Channels:   1,
	}
}

// Start begins capturing audio from a stream URL and returns a reader of raw PCM s16le data.
func (c *Capturer) Start(ctx context.Context, streamURL string) (io.ReadCloser, error) {
	args := []string{
		"-i", streamURL,
		"-vn",                          // no video
		"-acodec", "pcm_s16le",         // raw PCM
		"-ar", fmt.Sprintf("%d", c.SampleRate),
		"-ac", fmt.Sprintf("%d", c.Channels),
		"-f", "s16le",                  // raw output format
		"-loglevel", "error",
		"-",                            // output to stdout
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ffmpeg: %w", err)
	}

	slog.Info("audio capture started (ffmpeg)", "url_prefix", streamURL[:min(80, len(streamURL))])

	go func() {
		<-ctx.Done()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		slog.Info("audio capture stopped")
	}()

	return stdout, nil
}
