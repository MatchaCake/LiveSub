package audio

import (
	"encoding/binary"
	"io"
)

// AnalyzingReader wraps a PCM s16le reader and feeds audio to a MusicDetector.
// Reads pass through transparently to the caller (STT).
type AnalyzingReader struct {
	inner    io.ReadCloser
	detector *MusicDetector
	buf      []int16 // accumulate samples for analysis
	analyzeN int     // analyze every N samples
}

func NewAnalyzingReader(inner io.ReadCloser, detector *MusicDetector) *AnalyzingReader {
	return &AnalyzingReader{
		inner:    inner,
		detector: detector,
		buf:      make([]int16, 0, 4096),
		analyzeN: 3200, // ~200ms at 16kHz
	}
}

func (r *AnalyzingReader) Read(p []byte) (int, error) {
	n, err := r.inner.Read(p)
	if n > 0 {
		// Decode s16le samples and accumulate
		samples := n / 2
		for i := 0; i < samples; i++ {
			s := int16(binary.LittleEndian.Uint16(p[i*2 : i*2+2]))
			r.buf = append(r.buf, s)
		}

		// Analyze when enough samples accumulated
		if len(r.buf) >= r.analyzeN {
			r.detector.AnalyzeChunk(r.buf)
			r.buf = r.buf[:0]
		}
	}
	return n, err
}

func (r *AnalyzingReader) Close() error {
	return r.inner.Close()
}
