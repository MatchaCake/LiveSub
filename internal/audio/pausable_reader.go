package audio

import (
	"io"
	"time"
)

// PausableReader wraps a PCM reader and discards audio data when paused.
// This prevents audio from being sent to STT, saving API cost.
// The underlying reader (ffmpeg) keeps running to maintain the stream connection.
type PausableReader struct {
	inner    io.ReadCloser
	isPaused func() bool
}

func NewPausableReader(inner io.ReadCloser, isPaused func() bool) *PausableReader {
	return &PausableReader{inner: inner, isPaused: isPaused}
}

func (r *PausableReader) Read(p []byte) (int, error) {
	for r.isPaused() {
		// Read and discard audio to keep ffmpeg flowing
		buf := make([]byte, 3200) // 100ms of 16kHz 16-bit mono
		_, err := r.inner.Read(buf)
		if err != nil {
			return 0, err
		}
		time.Sleep(50 * time.Millisecond) // don't spin too fast
	}
	return r.inner.Read(p)
}

func (r *PausableReader) Close() error {
	return r.inner.Close()
}
