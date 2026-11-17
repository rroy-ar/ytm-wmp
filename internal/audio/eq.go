package audio

import (
	"encoding/binary"
	"io"
	"math"
	"sync"
)

// NumEQBands is the number of bands in the classic 10-band graphic equalizer.
const NumEQBands = 10

// EQMaxGainDB is the maximum boost/cut in decibels for preamp and each band.
const EQMaxGainDB = 12.0

// EQFrequencies lists the classic Winamp 10-band equalizer center frequencies (Hz).
var EQFrequencies = [NumEQBands]float64{
	60, 170, 310, 600, 1000, 3000, 6000, 12000, 14000, 16000,
}

const eqFilterQ = 1.4142135623730951 // sqrt(2), ~1 octave bandwidth

// EQSettings configures the 10-band equalizer and preamp.
type EQSettings struct {
	Enabled  bool
	PreampDB float64
	BandsDB  [NumEQBands]float64
}

// EQSliderToDB converts a [0, 100] UI slider value (50 = 0 dB) into [-12, +12] dB.
func EQSliderToDB(val float64) float64 {
	if math.IsNaN(val) || val <= 0 {
		return -EQMaxGainDB
	}
	if val >= 100 {
		return EQMaxGainDB
	}
	return (val - 50.0) / 50.0 * EQMaxGainDB
}

func clampDB(db float64) float64 {
	if math.IsNaN(db) {
		return 0
	}
	if db < -EQMaxGainDB {
		return -EQMaxGainDB
	}
	if db > EQMaxGainDB {
		return EQMaxGainDB
	}
	return db
}

type biquadCoeffs struct {
	b0, b1, b2 float64
	a1, a2     float64
}

type biquadState struct {
	z1L, z2L float64
	z1R, z2R float64
}

func peakingBiquad(freq, sampleRate, q, gainDB float64) (biquadCoeffs, bool) {
	if math.Abs(gainDB) < 1e-4 {
		return biquadCoeffs{b0: 1}, false
	}
	a := math.Pow(10, gainDB/40.0)
	w0 := 2.0 * math.Pi * freq / sampleRate
	cosW0 := math.Cos(w0)
	alpha := math.Sin(w0) / (2.0 * q)

	b0 := 1.0 + alpha*a
	b1 := -2.0 * cosW0
	b2 := 1.0 - alpha*a
	a0 := 1.0 + alpha/a
	a1 := -2.0 * cosW0
	a2 := 1.0 - alpha/a

	return biquadCoeffs{
		b0: b0 / a0,
		b1: b1 / a0,
		b2: b2 / a0,
		a1: a1 / a0,
		a2: a2 / a0,
	}, true
}

// EQReader wraps an f32le stereo PCM io.Reader with a 10-band biquad EQ and preamp.
type EQReader struct {
	src        io.Reader
	sampleRate float64

	mu       sync.Mutex
	settings EQSettings
	preamp   float64
	coeffs   [NumEQBands]biquadCoeffs
	active   [NumEQBands]bool
	states   [NumEQBands]biquadState

	inBuf  [FrameSize]byte
	inLen  int
	outBuf [FrameSize]byte
	outLen int
}

// NewEQReader creates an EQReader over src at SampleRate (48 kHz) with the given settings.
func NewEQReader(src io.Reader, settings EQSettings) *EQReader {
	r := &EQReader{
		src:        src,
		sampleRate: SampleRate,
	}
	r.SetEQ(settings)
	return r
}

// SetEQ updates the equalizer settings live. Safe for concurrent use with Read.
func (r *EQReader) SetEQ(s EQSettings) {
	s.PreampDB = clampDB(s.PreampDB)
	for i := range s.BandsDB {
		s.BandsDB[i] = clampDB(s.BandsDB[i])
	}

	r.mu.Lock()
	wasEnabled := r.settings.Enabled
	r.settings = s
	r.preamp = math.Pow(10, s.PreampDB/20.0)
	for i, freq := range EQFrequencies {
		c, active := peakingBiquad(freq, r.sampleRate, eqFilterQ, s.BandsDB[i])
		r.coeffs[i] = c
		r.active[i] = active
	}
	if !wasEnabled && s.Enabled {
		r.states = [NumEQBands]biquadState{}
	}
	r.mu.Unlock()
}

// Settings returns a snapshot of the current EQ settings.
func (r *EQReader) Settings() EQSettings {
	r.mu.Lock()
	s := r.settings
	r.mu.Unlock()
	return s
}

// Read reads PCM bytes from the underlying reader and applies EQ when enabled.
func (r *EQReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	if r.outLen > 0 {
		n := copy(p, r.outBuf[:r.outLen])
		copy(r.outBuf[:], r.outBuf[n:r.outLen])
		r.outLen -= n
		return n, nil
	}

	r.mu.Lock()
	enabled := r.settings.Enabled
	r.mu.Unlock()

	if !enabled && r.inLen == 0 {
		return r.src.Read(p)
	}

	if len(p) < FrameSize || r.inLen > 0 {
		var readErr error
		for r.inLen < FrameSize {
			n, err := r.src.Read(r.inBuf[r.inLen:])
			r.inLen += n
			if err != nil {
				readErr = err
				break
			}
		}
		if r.inLen == FrameSize {
			r.processFrames(r.inBuf[:FrameSize])
			r.outBuf = r.inBuf
			r.outLen = FrameSize
			r.inLen = 0
		} else if r.inLen > 0 {
			copy(r.outBuf[:], r.inBuf[:r.inLen])
			r.outLen = r.inLen
			r.inLen = 0
		} else {
			return 0, readErr
		}
		n := copy(p, r.outBuf[:r.outLen])
		copy(r.outBuf[:], r.outBuf[n:r.outLen])
		r.outLen -= n
		if r.outLen == 0 {
			return n, readErr
		}
		return n, nil
	}

	aligned := (len(p) / FrameSize) * FrameSize
	n, err := r.src.Read(p[:aligned])
	if n <= 0 {
		return n, err
	}

	complete := (n / FrameSize) * FrameSize
	if complete > 0 {
		r.processFrames(p[:complete])
	}

	if tail := n - complete; tail > 0 {
		if err != nil {
			return n, err
		}
		copy(r.inBuf[:tail], p[complete:n])
		r.inLen = tail
		if complete == 0 {
			return r.Read(p)
		}
		return complete, nil
	}

	return complete, err
}

func (r *EQReader) processFrames(buf []byte) {
	r.mu.Lock()
	if !r.settings.Enabled {
		r.mu.Unlock()
		return
	}
	preamp := r.preamp
	coeffs := r.coeffs
	active := r.active
	states := r.states
	r.mu.Unlock()

	for off := 0; off+FrameSize <= len(buf); off += FrameSize {
		l := float64(math.Float32frombits(binary.LittleEndian.Uint32(buf[off : off+4])))
		rv := float64(math.Float32frombits(binary.LittleEndian.Uint32(buf[off+4 : off+8])))

		l *= preamp
		rv *= preamp

		for i := 0; i < NumEQBands; i++ {
			if !active[i] {
				continue
			}
			c := coeffs[i]
			st := &states[i]

			yL := c.b0*l + st.z1L
			st.z1L = c.b1*l - c.a1*yL + st.z2L
			st.z2L = c.b2*l - c.a2*yL
			l = yL

			yR := c.b0*rv + st.z1R
			st.z1R = c.b1*rv - c.a1*yR + st.z2R
			st.z2R = c.b2*rv - c.a2*yR
			rv = yR
		}

		binary.LittleEndian.PutUint32(buf[off:off+4], math.Float32bits(clampSample(l)))
		binary.LittleEndian.PutUint32(buf[off+4:off+8], math.Float32bits(clampSample(rv)))
	}

	r.mu.Lock()
	r.states = states
	r.mu.Unlock()
}

func clampSample(v float64) float32 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v > 1.0 {
		return 1.0
	}
	if v < -1.0 {
		return -1.0
	}
	return float32(v)
}
