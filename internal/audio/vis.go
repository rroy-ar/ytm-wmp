package audio

import (
	"encoding/binary"
	"io"
	"math"
	"sync"
)

const (
	// VisRingCapacity is the number of mono samples retained for visualization (~85ms at 48 kHz).
	VisRingCapacity = 4096
	// VisFFTSize is the radix-2 FFT window length used by ComputeSpectrum.
	VisFFTSize = 1024
	// VisBarCount is the number of spectrum bars across the 76px Winamp visualizer.
	VisBarCount = 19
)

// VisRing is a thread-safe circular buffer of mono float32 PCM samples.
type VisRing struct {
	mu    sync.Mutex
	buf   []float32
	write int
	count int
}

// NewVisRing creates a ring buffer with capacity samples (defaults to VisRingCapacity).
func NewVisRing(capacity int) *VisRing {
	if capacity <= 0 {
		capacity = VisRingCapacity
	}
	return &VisRing{buf: make([]float32, capacity)}
}

// Reset clears all buffered samples.
func (r *VisRing) Reset() {
	r.mu.Lock()
	clear(r.buf)
	r.write = 0
	r.count = 0
	r.mu.Unlock()
}

// Push appends mono samples into the ring buffer.
func (r *VisRing) Push(samples []float32) {
	if len(samples) == 0 {
		return
	}
	r.mu.Lock()
	capN := len(r.buf)
	if len(samples) >= capN {
		samples = samples[len(samples)-capN:]
		copy(r.buf, samples)
		r.write = 0
		r.count = capN
		r.mu.Unlock()
		return
	}
	for _, s := range samples {
		r.buf[r.write] = s
		r.write = (r.write + 1) % capN
		if r.count < capN {
			r.count++
		}
	}
	r.mu.Unlock()
}

// Latest copies up to len(dst) most-recent samples in chronological order (oldest first).
func (r *VisRing) Latest(dst []float32) int {
	if len(dst) == 0 {
		return 0
	}
	r.mu.Lock()
	n := r.count
	if n > len(dst) {
		n = len(dst)
	}
	if n == 0 {
		r.mu.Unlock()
		return 0
	}
	capN := len(r.buf)
	start := (r.write - n + capN) % capN
	first := capN - start
	if first >= n {
		copy(dst[:n], r.buf[start:start+n])
	} else {
		copy(dst[:first], r.buf[start:])
		copy(dst[first:n], r.buf[:n-first])
	}
	r.mu.Unlock()
	return n
}

// VisTapReader tees f32le stereo PCM frames read from src into a VisRing.
type VisTapReader struct {
	src    io.Reader
	ring   *VisRing
	rem    [FrameSize]byte
	remLen int
	mono   []float32
}

// NewVisTapReader creates a tee reader that forwards all bytes from src while
// pushing mono-downmixed samples into ring.
func NewVisTapReader(src io.Reader, ring *VisRing) *VisTapReader {
	return &VisTapReader{
		src:  src,
		ring: ring,
		mono: make([]float32, 0, 1024),
	}
}

// Read reads from src and tees complete stereo frames into the ring buffer.
func (t *VisTapReader) Read(p []byte) (int, error) {
	n, err := t.src.Read(p)
	if n > 0 && t.ring != nil {
		t.pushBytes(p[:n])
	}
	return n, err
}

func (t *VisTapReader) pushBytes(data []byte) {
	t.mono = t.mono[:0]
	if t.remLen > 0 {
		need := FrameSize - t.remLen
		if len(data) < need {
			copy(t.rem[t.remLen:], data)
			t.remLen += len(data)
			return
		}
		copy(t.rem[t.remLen:], data[:need])
		t.mono = append(t.mono, frameToMono(t.rem[:FrameSize]))
		t.remLen = 0
		data = data[need:]
	}

	complete := (len(data) / FrameSize) * FrameSize
	for off := 0; off < complete; off += FrameSize {
		t.mono = append(t.mono, frameToMono(data[off:off+FrameSize]))
	}
	if tail := len(data) - complete; tail > 0 {
		copy(t.rem[:], data[complete:])
		t.remLen = tail
	}
	if len(t.mono) > 0 {
		t.ring.Push(t.mono)
	}
}

func frameToMono(frame []byte) float32 {
	l := math.Float32frombits(binary.LittleEndian.Uint32(frame[0:4]))
	r := math.Float32frombits(binary.LittleEndian.Uint32(frame[4:8]))
	s := (l + r) * 0.5
	if math.IsNaN(float64(s)) || math.IsInf(float64(s), 0) {
		return 0
	}
	return s
}

var hannWindow1024 = func() [VisFFTSize]float64 {
	var w [VisFFTSize]float64
	for i := range w {
		w[i] = 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(i)/float64(VisFFTSize-1)))
	}
	return w
}()

// ComputeSpectrum runs a Hann-windowed 1024-point FFT on samples and writes
// normalized [0, 1] logarithmic band energies into bars.
func ComputeSpectrum(samples []float32, bars []float64) {
	if len(bars) == 0 {
		return
	}
	if len(samples) == 0 {
		clear(bars)
		return
	}

	var re, im [VisFFTSize]float64
	if len(samples) > VisFFTSize {
		samples = samples[len(samples)-VisFFTSize:]
	}
	offset := VisFFTSize - len(samples)
	for i, s := range samples {
		re[offset+i] = float64(s) * hannWindow1024[offset+i]
	}

	fftRadix2(re[:], im[:])

	// Map positive bins [1 .. VisFFTSize/2-1] across log-spaced frequencies
	// from 60 Hz to 16 kHz.
	const minFreq = 60.0
	const maxFreq = 16000.0
	binHz := float64(SampleRate) / float64(VisFFTSize)
	maxBin := VisFFTSize/2 - 1
	numBars := len(bars)

	for b := 0; b < numBars; b++ {
		fLow := minFreq * math.Pow(maxFreq/minFreq, float64(b)/float64(numBars))
		fHigh := minFreq * math.Pow(maxFreq/minFreq, float64(b+1)/float64(numBars))
		k0 := int(math.Floor(fLow / binHz))
		k1 := int(math.Ceil(fHigh / binHz))
		if k0 < 1 {
			k0 = 1
		}
		if k1 <= k0 {
			k1 = k0 + 1
		}
		if k1 > maxBin {
			k1 = maxBin
		}
		if k0 > maxBin {
			k0 = maxBin
		}

		peakMag := 0.0
		for k := k0; k <= k1; k++ {
			mag := math.Hypot(re[k], im[k])
			if mag > peakMag {
				peakMag = mag
			}
		}

		// Normalize by N/4 (Hann coherent gain ~0.5 * N/2) and map [-60 dB, 0 dB] -> [0, 1].
		norm := peakMag / (float64(VisFFTSize) * 0.25)
		if norm < 1e-6 {
			bars[b] = 0
			continue
		}
		db := 20.0 * math.Log10(norm)
		val := (db + 60.0) / 60.0
		if val < 0 {
			val = 0
		} else if val > 1 {
			val = 1
		}
		bars[b] = val
	}
}

func fftRadix2(re, im []float64) {
	n := len(re)
	// Bit-reversal permutation.
	j := 0
	for i := 1; i < n; i++ {
		bit := n >> 1
		for j&bit != 0 {
			j ^= bit
			bit >>= 1
		}
		j ^= bit
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}

	// Butterfly stages.
	for size := 2; size <= n; size <<= 1 {
		half := size >> 1
		angle := -2.0 * math.Pi / float64(size)
		wStepRe := math.Cos(angle)
		wStepIm := math.Sin(angle)
		for start := 0; start < n; start += size {
			wRe, wIm := 1.0, 0.0
			for k := 0; k < half; k++ {
				even := start + k
				odd := even + half
				tRe := wRe*re[odd] - wIm*im[odd]
				tIm := wRe*im[odd] + wIm*re[odd]
				re[odd] = re[even] - tRe
				im[odd] = im[even] - tIm
				re[even] += tRe
				im[even] += tIm
				wRe, wIm = wRe*wStepRe-wIm*wStepIm, wRe*wStepIm+wIm*wStepRe
			}
		}
	}
}
