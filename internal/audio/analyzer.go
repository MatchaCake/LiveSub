package audio

import (
	"math"
	"math/cmplx"
	"sync"
)

// MusicDetector analyzes PCM audio to detect background music vs speech.
// Uses spectral analysis: BGM has sustained low-frequency energy and flat spectrum.
type MusicDetector struct {
	sampleRate int
	windowSize int     // FFT window size in samples
	hopSize    int     // samples between analyses
	threshold  float64 // music score threshold (0-1)

	mu       sync.RWMutex
	isMusic  bool
	score    float64 // current music score (0=speech, 1=music)
	history  []float64
	histSize int
}

func NewMusicDetector(sampleRate int) *MusicDetector {
	return &MusicDetector{
		sampleRate: sampleRate,
		windowSize: 2048,        // ~128ms at 16kHz
		hopSize:    1024,        // ~64ms hop
		threshold:  0.45,        // balanced: catch BGM but recover fast
		history:    make([]float64, 0, 16),
		histSize:   6,           // ~400ms window, recovers quickly after music stops
	}
}

// IsMusic returns whether background music is currently detected.
func (d *MusicDetector) IsMusic() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.isMusic
}

// Score returns the current music score (0=speech, 1=music).
func (d *MusicDetector) Score() float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.score
}

// AnalyzeChunk processes a chunk of PCM s16le samples and updates the music detection state.
// samples should be int16 values.
func (d *MusicDetector) AnalyzeChunk(samples []int16) {
	if len(samples) < d.windowSize {
		return
	}

	// Analyze last window in chunk
	offset := len(samples) - d.windowSize
	window := samples[offset : offset+d.windowSize]

	// Apply Hann window and convert to float
	floats := make([]float64, d.windowSize)
	for i, s := range window {
		hann := 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(d.windowSize-1)))
		floats[i] = float64(s) / 32768.0 * hann
	}

	// FFT
	spectrum := fft(floats)
	magnitudes := make([]float64, len(spectrum)/2)
	for i := range magnitudes {
		magnitudes[i] = cmplx.Abs(spectrum[i])
	}

	// Feature 1: Low frequency energy ratio (0-300Hz vs total)
	// At 16kHz sample rate, bin resolution = 16000/2048 ≈ 7.8Hz
	lowBinEnd := int(300.0 / (float64(d.sampleRate) / float64(d.windowSize)))  // ~38
	midBinStart := int(300.0 / (float64(d.sampleRate) / float64(d.windowSize)))
	midBinEnd := int(3000.0 / (float64(d.sampleRate) / float64(d.windowSize))) // ~384

	var lowEnergy, midEnergy, totalEnergy float64
	for i, m := range magnitudes {
		e := m * m
		totalEnergy += e
		if i < lowBinEnd {
			lowEnergy += e
		}
		if i >= midBinStart && i < midBinEnd {
			midEnergy += e
		}
	}

	if totalEnergy < 1e-10 {
		return // silence
	}

	lowRatio := lowEnergy / totalEnergy

	// Feature 2: Spectral flatness (geometric mean / arithmetic mean)
	// Music → flatter (closer to 1), Speech → peaky (closer to 0)
	var logSum float64
	var arithSum float64
	count := 0
	for _, m := range magnitudes {
		if m > 1e-10 {
			logSum += math.Log(m)
			arithSum += m
			count++
		}
	}

	flatness := 0.0
	if count > 0 && arithSum > 0 {
		geoMean := math.Exp(logSum / float64(count))
		arithMean := arithSum / float64(count)
		flatness = geoMean / arithMean
	}

	// Feature 3: Energy stability (low variance = sustained sound = music)
	// We use the ratio of mid-band energy to check for sustained harmonic content
	midRatio := midEnergy / totalEnergy

	// Combine features into music score
	// High low-freq ratio + high flatness + balanced mid = music
	score := 0.0
	score += clamp(lowRatio*3.0, 0, 1) * 0.4       // low freq presence (BGM bass/drums)
	score += clamp(flatness*2.5, 0, 1) * 0.35       // spectral flatness (full spectrum = music)
	score += clamp((1-midRatio)*2.0, 0, 1) * 0.25   // energy spread beyond voice band

	// Update history for smoothing
	d.mu.Lock()
	d.history = append(d.history, score)
	if len(d.history) > d.histSize {
		d.history = d.history[len(d.history)-d.histSize:]
	}

	// Smoothed score
	avg := 0.0
	for _, s := range d.history {
		avg += s
	}
	avg /= float64(len(d.history))

	d.score = avg
	d.isMusic = avg > d.threshold
	d.mu.Unlock()
}

// --- FFT (radix-2 Cooley-Tukey) ---

func fft(x []float64) []complex128 {
	n := len(x)
	// Pad to next power of 2
	n2 := 1
	for n2 < n {
		n2 <<= 1
	}

	c := make([]complex128, n2)
	for i := 0; i < n; i++ {
		c[i] = complex(x[i], 0)
	}

	fftInPlace(c)
	return c
}

func fftInPlace(a []complex128) {
	n := len(a)
	if n <= 1 {
		return
	}

	// Bit-reversal permutation
	j := 0
	for i := 1; i < n; i++ {
		bit := n >> 1
		for j&bit != 0 {
			j ^= bit
			bit >>= 1
		}
		j ^= bit
		if i < j {
			a[i], a[j] = a[j], a[i]
		}
	}

	// Cooley-Tukey
	for size := 2; size <= n; size <<= 1 {
		half := size / 2
		w := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))
		for start := 0; start < n; start += size {
			wn := complex(1, 0)
			for k := 0; k < half; k++ {
				t := wn * a[start+k+half]
				a[start+k+half] = a[start+k] - t
				a[start+k] = a[start+k] + t
				wn *= w
			}
		}
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
