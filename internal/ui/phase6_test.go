package ui

import (
	"context"
	"math"
	"path/filepath"
	"testing"

	"github.com/rroy-ar/ytm-wmp/internal/audio"
)

type fakeEQVisSink struct {
	fakeAudioLifecycle
	eqCalls []audio.EQSettings
	samples []float32
}

func (s *fakeEQVisSink) SetEQ(settings audio.EQSettings) {
	s.eqCalls = append(s.eqCalls, settings)
}

func (s *fakeEQVisSink) ReadVisSamples(dst []float32) int {
	n := len(s.samples)
	if n > len(dst) {
		n = len(dst)
	}
	copy(dst, s.samples[:n])
	return n
}

func TestPhase6AEQLiveWiring(t *testing.T) {
	sink := &fakeEQVisSink{}
	eqPath := filepath.Join(t.TempDir(), "eq.json")
	g := NewGame(context.Background(), nil, newFakeController(), sink, WithEQPath(eqPath))
	defer g.Close()

	if len(sink.eqCalls) != 1 {
		t.Fatalf("initial SetEQ calls = %d, want 1", len(sink.eqCalls))
	}
	if sink.eqCalls[0].Enabled {
		t.Fatalf("expected initial EQ disabled")
	}

	g.onClick(WidgetEQOn)
	if len(sink.eqCalls) != 2 || !sink.eqCalls[1].Enabled {
		t.Fatalf("expected SetEQ(Enabled=true) after WidgetEQOn, got %+v", sink.eqCalls)
	}

	g.onClick(WidgetEQPlus12)
	last := sink.eqCalls[len(sink.eqCalls)-1]
	for i, db := range last.BandsDB {
		if math.Abs(db-12.0) > 1e-6 {
			t.Fatalf("band %d after +12 = %v, want 12", i, db)
		}
	}

	g.onClick(WidgetEQMinus12)
	last = sink.eqCalls[len(sink.eqCalls)-1]
	for i, db := range last.BandsDB {
		if math.Abs(db+12.0) > 1e-6 {
			t.Fatalf("band %d after -12 = %v, want -12", i, db)
		}
	}

	g.onClick(WidgetEQPresets)
	last = sink.eqCalls[len(sink.eqCalls)-1]
	if last.BandsDB[0] <= 0 {
		t.Fatalf("expected ROCK preset (index 1) to boost band 0, got %v", last.BandsDB[0])
	}
}

func TestPhase6BVisualizerModesAndUpdate(t *testing.T) {
	samples := make([]float32, audio.VisFFTSize)
	for i := range samples {
		samples[i] = float32(math.Sin(2 * math.Pi * 1000.0 * float64(i) / float64(audio.SampleRate)))
	}
	sink := &fakeEQVisSink{samples: samples}
	g := NewGame(context.Background(), nil, newFakeController(), sink)
	defer g.Close()

	if g.visMode != visModeSpectrum {
		t.Fatalf("initial visMode = %v, want visModeSpectrum", g.visMode)
	}

	g.updateVisualizer(true)
	maxBar := 0.0
	for _, b := range g.visBars {
		if b > maxBar {
			maxBar = b
		}
	}
	if maxBar < 0.5 {
		t.Fatalf("expected spectrum bar peak > 0.5 for 1kHz sine, got %v", maxBar)
	}

	// Pausing decays bars.
	prevMax := maxBar
	g.updateVisualizer(false)
	newMax := 0.0
	for _, b := range g.visBars {
		if b > newMax {
			newMax = b
		}
	}
	if newMax >= prevMax {
		t.Fatalf("expected bars to decay when not playing: prev=%v new=%v", prevMax, newMax)
	}

	// Cycle Spectrum -> Oscilloscope -> Off -> Spectrum.
	g.onClick(WidgetMainVis)
	if g.visMode != visModeOscilloscope {
		t.Fatalf("visMode after 1st click = %v, want visModeOscilloscope", g.visMode)
	}
	g.updateVisualizer(true)
	nonZeroScope := false
	for _, v := range g.visScope {
		if math.Abs(v) > 0.05 {
			nonZeroScope = true
			break
		}
	}
	if !nonZeroScope {
		t.Fatalf("expected non-zero oscilloscope samples when playing")
	}

	g.onClick(WidgetMainVis)
	if g.visMode != visModeOff {
		t.Fatalf("visMode after 2nd click = %v, want visModeOff", g.visMode)
	}

	g.onClick(WidgetMainVis)
	if g.visMode != visModeSpectrum {
		t.Fatalf("visMode after 3rd click = %v, want visModeSpectrum", g.visMode)
	}
}
