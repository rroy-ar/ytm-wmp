package audio

import (
	"bytes"
	"io"
	"math"
	"sync"
	"testing"
)

func TestVisRingWrapAndLatest(t *testing.T) {
	r := NewVisRing(8)
	r.Push([]float32{1, 2, 3, 4, 5})
	dst := make([]float32, 4)
	n := r.Latest(dst)
	if n != 4 {
		t.Fatalf("Latest n = %d, want 4", n)
	}
	want := []float32{2, 3, 4, 5}
	for i := range want {
		if dst[i] != want[i] {
			t.Fatalf("dst[%d] = %v, want %v", i, dst[i], want[i])
		}
	}

	// Push 6 more to force wrap-around.
	r.Push([]float32{6, 7, 8, 9, 10, 11})
	all := make([]float32, 8)
	n = r.Latest(all)
	if n != 8 {
		t.Fatalf("Latest n = %d, want 8", n)
	}
	wantAll := []float32{4, 5, 6, 7, 8, 9, 10, 11}
	for i := range wantAll {
		if all[i] != wantAll[i] {
			t.Fatalf("all[%d] = %v, want %v", i, all[i], wantAll[i])
		}
	}

	r.Reset()
	if n := r.Latest(all); n != 0 {
		t.Fatalf("Latest after Reset = %d, want 0", n)
	}
}

func TestVisTapReaderUnaligned(t *testing.T) {
	pcm := makeSinePCM(1000, 64, 0.5)
	ring := NewVisRing(128)
	tap := NewVisTapReader(&chunkedReader{data: pcm, chunk: 5}, ring)

	out, err := io.ReadAll(tap)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(out, pcm) {
		t.Fatalf("VisTapReader modified bytes")
	}
	got := make([]float32, 64)
	n := ring.Latest(got)
	if n != 64 {
		t.Fatalf("ring Latest = %d, want 64", n)
	}
}

func TestComputeSpectrumSinePeak(t *testing.T) {
	// Generate 1024 samples of a 1 kHz sine and verify mid-band bars peak higher
	// than low-band and high-band bars.
	samples := make([]float32, VisFFTSize)
	for i := range samples {
		samples[i] = float32(math.Sin(2 * math.Pi * 1000.0 * float64(i) / float64(SampleRate)))
	}
	var bars [VisBarCount]float64
	ComputeSpectrum(samples, bars[:])

	peakIdx := 0
	peakVal := -1.0
	for i, v := range bars {
		if v > peakVal {
			peakVal = v
			peakIdx = i
		}
	}
	// 1 kHz in log-space [60..16000] Hz over 19 bars is around bar 9..10.
	if peakIdx < 7 || peakIdx > 11 {
		t.Fatalf("1kHz peak bar = %d (val=%.3f), bars=%v", peakIdx, peakVal, bars)
	}
	if peakVal < 0.7 {
		t.Fatalf("1kHz peak bar value = %.3f, want > 0.7", peakVal)
	}
	if bars[0] > 0.3 || bars[VisBarCount-1] > 0.3 {
		t.Fatalf("expected low/high edge bars to be quiet, got first=%.3f last=%.3f", bars[0], bars[VisBarCount-1])
	}

	// Silence -> all zeros.
	ComputeSpectrum(make([]float32, VisFFTSize), bars[:])
	for i, v := range bars {
		if v != 0 {
			t.Fatalf("silence bar[%d] = %v, want 0", i, v)
		}
	}
}

func TestVisRingConcurrent(t *testing.T) {
	ring := NewVisRing(512)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		chunk := make([]float32, 64)
		for i := 0; i < 200; i++ {
			ring.Push(chunk)
		}
	}()
	go func() {
		defer wg.Done()
		dst := make([]float32, 256)
		for i := 0; i < 200; i++ {
			_ = ring.Latest(dst)
		}
	}()
	wg.Wait()
}
