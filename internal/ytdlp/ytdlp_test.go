package ytdlp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeRunner struct {
	mu       sync.Mutex
	calls    int
	lastArgs []string
	fn       func(call int) (stdout, stderr []byte, err error)
}

func (f *fakeRunner) Output(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
	f.mu.Lock()
	f.calls++
	f.lastArgs = append([]string(nil), args...)
	call := f.calls
	fn := f.fn
	f.mu.Unlock()

	if fn != nil {
		return fn(call)
	}
	return []byte("https://stream.example/audio\n"), nil, nil
}

func (f *fakeRunner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeRunner) args() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.lastArgs...)
}

func TestResolveCachesByVideoID(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	runner := &fakeRunner{}
	resolver := New(WithRunner(runner), WithClock(func() time.Time { return now }))

	first, err := resolver.Resolve(context.Background(), "https://youtu.be/dQw4w9WgXcQ?si=tracking")
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	second, err := resolver.Resolve(context.Background(), "dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}

	if got := runner.callCount(); got != 1 {
		t.Fatalf("runner calls = %d, want 1 (URL and bare ID must share the cache entry)", got)
	}
	if first.URL != second.URL {
		t.Fatalf("URLs differ: %q vs %q", first.URL, second.URL)
	}
	if want := now.Add(DefaultTTL); !first.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", first.ExpiresAt, want)
	}
}

func TestResolveCacheExpiry(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	runner := &fakeRunner{}
	resolver := New(WithRunner(runner), WithClock(func() time.Time { return now }))

	if _, err := resolver.Resolve(context.Background(), "dQw4w9WgXcQ"); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	now = now.Add(DefaultTTL - time.Second)
	if _, err := resolver.Resolve(context.Background(), "dQw4w9WgXcQ"); err != nil {
		t.Fatalf("resolve before expiry: %v", err)
	}
	if got := runner.callCount(); got != 1 {
		t.Fatalf("runner calls before expiry = %d, want 1", got)
	}

	now = now.Add(2 * time.Second)
	info, err := resolver.Resolve(context.Background(), "dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("resolve after expiry: %v", err)
	}
	if got := runner.callCount(); got != 2 {
		t.Fatalf("runner calls after expiry = %d, want 2", got)
	}
	if want := now.Add(DefaultTTL); !info.ExpiresAt.Equal(want) {
		t.Fatalf("refreshed ExpiresAt = %v, want %v", info.ExpiresAt, want)
	}
}

func TestForgetDropsCachedURL(t *testing.T) {
	runner := &fakeRunner{}
	resolver := New(WithRunner(runner))

	if _, err := resolver.Resolve(context.Background(), "dQw4w9WgXcQ"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	resolver.Forget("https://www.youtube.com/watch?v=dQw4w9WgXcQ")

	if _, err := resolver.Resolve(context.Background(), "dQw4w9WgXcQ"); err != nil {
		t.Fatalf("resolve after forget: %v", err)
	}
	if got := runner.callCount(); got != 2 {
		t.Fatalf("runner calls = %d, want 2 after Forget", got)
	}
}

func TestTTLZeroDisablesCache(t *testing.T) {
	runner := &fakeRunner{}
	resolver := New(WithRunner(runner), WithTTL(0))

	for i := 0; i < 2; i++ {
		info, err := resolver.Resolve(context.Background(), "dQw4w9WgXcQ")
		if err != nil {
			t.Fatalf("resolve %d: %v", i, err)
		}
		if !info.ExpiresAt.IsZero() {
			t.Fatalf("ExpiresAt = %v, want zero when caching is disabled", info.ExpiresAt)
		}
	}
	if got := runner.callCount(); got != 2 {
		t.Fatalf("runner calls = %d, want 2", got)
	}
}

func TestResolveBareIDUsesWatchURL(t *testing.T) {
	runner := &fakeRunner{}
	resolver := New(WithRunner(runner))

	if _, err := resolver.Resolve(context.Background(), "dQw4w9WgXcQ"); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	want := []string{
		"-f", "bestaudio/best",
		"--no-warnings",
		"--no-playlist",
		"-g", "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
	}
	got := runner.args()
	if len(got) != len(want) {
		t.Fatalf("args = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %q, want %q", got, want)
		}
	}
}

func TestResolveErrorIncludesStderr(t *testing.T) {
	runner := &fakeRunner{fn: func(int) ([]byte, []byte, error) {
		return nil, []byte("ERROR: video unavailable\n"), errors.New("exit status 1")
	}}
	resolver := New(WithRunner(runner))

	_, err := resolver.Resolve(context.Background(), "dQw4w9WgXcQ")
	if !errors.Is(err, ErrResolve) {
		t.Fatalf("error = %v, want ErrResolve", err)
	}
	if !strings.Contains(err.Error(), "video unavailable") {
		t.Fatalf("error = %v, want yt-dlp stderr", err)
	}
}

func TestResolveEmptyOutput(t *testing.T) {
	runner := &fakeRunner{fn: func(int) ([]byte, []byte, error) {
		return []byte("\n \n"), nil, nil
	}}
	resolver := New(WithRunner(runner))

	_, err := resolver.Resolve(context.Background(), "dQw4w9WgXcQ")
	if !errors.Is(err, ErrResolve) {
		t.Fatalf("error = %v, want ErrResolve", err)
	}
}

func TestResolveReturnsContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runner := &fakeRunner{fn: func(int) ([]byte, []byte, error) {
		return nil, nil, context.Canceled
	}}
	resolver := New(WithRunner(runner))

	_, err := resolver.Resolve(ctx, "dQw4w9WgXcQ")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if errors.Is(err, ErrResolve) {
		t.Fatalf("error = %v, must not be wrapped as ErrResolve", err)
	}
}

func TestVideoID(t *testing.T) {
	tests := []struct {
		target string
		want   string
	}{
		{"dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"https://youtu.be/dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=42", "dQw4w9WgXcQ"},
		{"https://music.youtube.com/watch?v=dQw4w9WgXcQ&list=RDAMVM", "dQw4w9WgXcQ"},
		{"https://youtube.com/shorts/dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"https://www.youtube.com/embed/dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"https://www.youtube.com/live/dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"daft punk one more time", ""},
		{"https://example.com/watch?v=dQw4w9WgXcQ", ""},
		{"https://youtu.be/too-short", ""},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			if got := videoID(tt.target); got != tt.want {
				t.Fatalf("videoID(%q) = %q, want %q", tt.target, got, tt.want)
			}
		})
	}
}

func TestExecRunnerEnforcesStdoutCap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake yt-dlp is not available on Windows")
	}

	script := filepath.Join(t.TempDir(), "big-stdout")
	content := "#!/bin/sh\ndd if=/dev/zero bs=1024 count=16 2>/dev/null\n"
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	runner := ExecRunner{MaxBytes: 4096}
	_, _, err := runner.Output(context.Background(), script)
	if !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("Output err = %v, want ErrOutputTooLarge", err)
	}
}

func TestExecRunnerBoundsStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake yt-dlp is not available on Windows")
	}

	script := filepath.Join(t.TempDir(), "big-stderr")
	content := "#!/bin/sh\ndd if=/dev/zero bs=1024 count=128 >&2 2>/dev/null\nprintf 'https://stream.example/ok\\n'\n"
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	runner := ExecRunner{}
	stdout, stderr, err := runner.Output(context.Background(), script)
	if err != nil {
		t.Fatalf("Output err = %v", err)
	}
	if string(stdout) != "https://stream.example/ok\n" {
		t.Fatalf("stdout = %q", stdout)
	}
	if len(stderr) != defaultStderrLimit {
		t.Fatalf("len(stderr) = %d, want %d", len(stderr), defaultStderrLimit)
	}
}

func TestResolverSetBinaryUpdatesInvocation(t *testing.T) {
	var seenBinary string
	runner := &fakeRunner{fn: func(int) ([]byte, []byte, error) {
		return []byte("https://stream.example/audio\n"), nil, nil
	}}
	// Wrap fakeRunner to capture the binary name passed to Output.
	capRunner := &binaryCaptureRunner{inner: runner, onCall: func(bin string) { seenBinary = bin }}
	resolver := New(WithRunner(capRunner), WithTTL(0))
	if got := resolver.Binary(); got != defaultBinary {
		t.Fatalf("initial Binary() = %q, want %q", got, defaultBinary)
	}
	resolver.SetBinary("/custom/bin/yt-dlp")
	if got := resolver.Binary(); got != "/custom/bin/yt-dlp" {
		t.Fatalf("updated Binary() = %q", got)
	}
	if _, err := resolver.Resolve(context.Background(), "dQw4w9WgXcQ"); err != nil {
		t.Fatalf("Resolve = %v", err)
	}
	if seenBinary != "/custom/bin/yt-dlp" {
		t.Fatalf("runner binary = %q, want /custom/bin/yt-dlp", seenBinary)
	}
	resolver.SetBinary("")
	if got := resolver.Binary(); got != defaultBinary {
		t.Fatalf("reset Binary() = %q, want %q", got, defaultBinary)
	}
}

type binaryCaptureRunner struct {
	inner  Runner
	onCall func(string)
}

func (b *binaryCaptureRunner) Output(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	if b.onCall != nil {
		b.onCall(name)
	}
	return b.inner.Output(ctx, name, args...)
}

type ctxRunner struct {
	fn func(ctx context.Context) ([]byte, []byte, error)
}

func (r *ctxRunner) Output(ctx context.Context, _ string, _ ...string) ([]byte, []byte, error) {
	return r.fn(ctx)
}

func TestResolveAppliesDefaultTimeout(t *testing.T) {
	var hasDeadline bool
	var remaining time.Duration
	runner := &ctxRunner{fn: func(ctx context.Context) ([]byte, []byte, error) {
		var deadline time.Time
		deadline, hasDeadline = ctx.Deadline()
		if hasDeadline {
			remaining = time.Until(deadline)
		}
		return []byte("https://stream.example/audio\n"), nil, nil
	}}

	resolver := New(WithRunner(runner))
	if _, err := resolver.Resolve(context.Background(), "dQw4w9WgXcQ"); err != nil {
		t.Fatalf("Resolve = %v", err)
	}
	if !hasDeadline {
		t.Fatal("expected Resolve context to have a deadline")
	}
	if remaining <= 0 || remaining > DefaultResolveTimeout+time.Second {
		t.Fatalf("deadline remaining = %v, want near %v", remaining, DefaultResolveTimeout)
	}
}

func TestResolveTimeoutZeroDisablesDeadline(t *testing.T) {
	var hasDeadline bool
	runner := &ctxRunner{fn: func(ctx context.Context) ([]byte, []byte, error) {
		_, hasDeadline = ctx.Deadline()
		return []byte("https://stream.example/audio\n"), nil, nil
	}}

	resolver := New(WithRunner(runner), WithTimeout(0))
	if _, err := resolver.Resolve(context.Background(), "dQw4w9WgXcQ"); err != nil {
		t.Fatalf("Resolve = %v", err)
	}
	if hasDeadline {
		t.Fatal("expected WithTimeout(0) to leave context without deadline")
	}
}

func TestResolveTimeoutReturnsDeadlineExceeded(t *testing.T) {
	runner := &ctxRunner{fn: func(ctx context.Context) ([]byte, []byte, error) {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}}

	resolver := New(WithRunner(runner), WithTimeout(20*time.Millisecond))
	_, err := resolver.Resolve(context.Background(), "dQw4w9WgXcQ")
	if !errors.Is(err, ErrResolve) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want ErrResolve + context.DeadlineExceeded", err)
	}
}

func TestResolveTimeoutKillsRealSubprocess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake yt-dlp is not available on Windows")
	}

	script := filepath.Join(t.TempDir(), "slow-yt-dlp")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec sleep 60\n"), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	resolver := New(WithBinary(script), WithTimeout(50*time.Millisecond))
	start := time.Now()
	_, err := resolver.Resolve(context.Background(), "dQw4w9WgXcQ")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timed-out Resolve took %v, expected prompt kill", elapsed)
	}
}
