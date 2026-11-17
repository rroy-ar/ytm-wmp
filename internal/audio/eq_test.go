package audio

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"sync"
	"testing"
)

func makeSinePCM(freq float64, frames int, amp float32) []byte {
	buf := make([]byte, frames*FrameSize)
	for i := 0; i < frames; i++ {
		s := amp * float32(math.Sin(2*math.Pi*freq*float64(i)/float64(SampleRate)))
		off := i * FrameSize
		binary.LittleEndian.PutUint32(buf[off:off+4], math.Float32bits(s))
		binary.LittleEndian.PutUint32(buf[off+4:off+8], math.Float32bits(s))
	}
	return buf
}

func pcmRMS(buf []byte) float64 {
	var sum float64
	frames := len(buf) / FrameSize
	if frames == 0 {
		return 0
	}
	for i := 0; i < frames; i++ {
		off := i * FrameSize
		l := float64(math.Float32frombits(binary.LittleEndian.Uint32(buf[off : off+4])))
		sum += l * l
	}
	return math.Sqrt(sum / float64(frames))
}

func TestEQSliderToDB(t *testing.T) {
	if got := EQSliderToDB(0); got != -12 {
		t.Fatalf("EQSliderToDB(0) = %v, want -12", got)
	}
	if got := EQSliderToDB(50); got != 0 {
		t.Fatalf("EQSliderToDB(50) = %v, want 0", got)
	}
	if got := EQSliderToDB(100); got != 12 {
		t.Fatalf("EQSliderToDB(100) = %v, want 12", got)
	}
	if got := EQSliderToDB(math.NaN()); got != -12 {
		t.Fatalf("EQSliderToDB(NaN) = %v, want -12", got)
	}
}

func TestEQReaderBypass(t *testing.T) {
	in := makeSinePCM(1000, 512, 0.5)
	r := NewEQReader(bytes.NewReader(in), EQSettings{
		Enabled:  false,
		PreampDB: 12,
		BandsDB:  [NumEQBands]float64{12, 12, 12, 12, 12, 12, 12, 12, 12, 12},
	})
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(out, in) {
		t.Fatalf("bypass modified PCM bytes")
	}
}

func TestEQReaderFlatUnity(t *testing.T) {
	in := makeSinePCM(440, 512, 0.5)
	r := NewEQReader(bytes.NewReader(in), EQSettings{Enabled: true})
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("len(out) = %d, want %d", len(out), len(in))
	}
	for i := 0; i < len(in); i += 4 {
		a := math.Float32frombits(binary.LittleEndian.Uint32(in[i : i+4]))
		b := math.Float32frombits(binary.LittleEndian.Uint32(out[i : i+4]))
		if math.Abs(float64(a-b)) > 1e-6 {
			t.Fatalf("sample %d changed in flat EQ: got %v, want %v", i/4, b, a)
		}
	}
}

func TestEQReaderPreampAndClipping(t *testing.T) {
	in := makeSinePCM(1000, 256, 0.8)
	r := NewEQReader(bytes.NewReader(in), EQSettings{
		Enabled:  true,
		PreampDB: 12,
	})
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	clipped := 0
	for i := 0; i < len(out); i += 4 {
		s := math.Float32frombits(binary.LittleEndian.Uint32(out[i : i+4]))
		if math.IsNaN(float64(s)) || s > 1.0 || s < -1.0 {
			t.Fatalf("sample %d out of [-1,1]: %v", i/4, s)
		}
		if s == 1.0 || s == -1.0 {
			clipped++
		}
	}
	if clipped == 0 {
		t.Fatalf("expected +12dB preamp on 0.8 amplitude to clip some samples")
	}
}

func TestEQReaderBandBoostAndCut(t *testing.T) {
	// Skip initial transient (first 1024 frames) when measuring steady-state RMS.
	const totalFrames = 4096
	const skipBytes = 1024 * FrameSize

	lowIn := makeSinePCM(60, totalFrames, 0.1)
	highIn := makeSinePCM(6000, totalFrames, 0.1)

	var boost60 EQSettings
	boost60.Enabled = true
	boost60.BandsDB[0] = 12 // 60 Hz +12 dB

	lowOut, err := io.ReadAll(NewEQReader(bytes.NewReader(lowIn), boost60))
	if err != nil {
		t.Fatalf("ReadAll low: %v", err)
	}
	highOut, err := io.ReadAll(NewEQReader(bytes.NewReader(highIn), boost60))
	if err != nil {
		t.Fatalf("ReadAll high: %v", err)
	}

	lowGain := pcmRMS(lowOut[skipBytes:]) / pcmRMS(lowIn[skipBytes:])
	highGain := pcmRMS(highOut[skipBytes:]) / pcmRMS(highIn[skipBytes:])
	if lowGain < 3.0 {
		t.Fatalf("60Hz +12dB gain = %.2f, want > 3.0", lowGain)
	}
	if highGain > 1.15 {
		t.Fatalf("6kHz gain with 60Hz boost = %.2f, want ~1.0", highGain)
	}

	var cut6k EQSettings
	cut6k.Enabled = true
	cut6k.BandsDB[6] = -12 // 6000 Hz -12 dB
	cutOut, err := io.ReadAll(NewEQReader(bytes.NewReader(highIn), cut6k))
	if err != nil {
		t.Fatalf("ReadAll cut: %v", err)
	}
	cutGain := pcmRMS(cutOut[skipBytes:]) / pcmRMS(highIn[skipBytes:])
	if cutGain > 0.35 {
		t.Fatalf("6kHz -12dB gain = %.2f, want < 0.35", cutGain)
	}
}

type chunkedReader struct {
	data  []byte
	chunk int
}

func (c *chunkedReader) Read(p []byte) (int, error) {
	if len(c.data) == 0 {
		return 0, io.EOF
	}
	n := c.chunk
	if n > len(p) {
		n = len(p)
	}
	if n > len(c.data) {
		n = len(c.data)
	}
	copy(p, c.data[:n])
	c.data = c.data[n:]
	return n, nil
}

func TestEQReaderUnalignedReads(t *testing.T) {
	in := makeSinePCM(310, 200, 0.25)
	s := EQSettings{Enabled: true, PreampDB: 3}
	s.BandsDB[2] = 6

	want, err := io.ReadAll(NewEQReader(bytes.NewReader(in), s))
	if err != nil {
		t.Fatalf("ReadAll aligned: %v", err)
	}

	r := NewEQReader(&chunkedReader{data: in, chunk: 5}, s)
	var got []byte
	buf := make([]byte, 3)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			got = append(got, buf[:n]...)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unaligned Read: %v", err)
		}
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("unaligned Read output differs from aligned Read (len got=%d want=%d)", len(got), len(want))
	}
}

func TestEQReaderConcurrentSetEQ(t *testing.T) {
	in := makeSinePCM(1000, 8192, 0.2)
	r := NewEQReader(bytes.NewReader(in), EQSettings{Enabled: true})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			s := EQSettings{Enabled: i%2 == 0, PreampDB: float64(i%12 - 6)}
			s.BandsDB[i%NumEQBands] = float64(i%24 - 12)
			r.SetEQ(s)
		}
	}()

	buf := make([]byte, 256)
	for {
		_, err := r.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
	}
	wg.Wait()
}
