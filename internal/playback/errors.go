package playback

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"

	"github.com/rroy-ar/ytm-wmp/internal/audio"
)

// Error taxonomy sentinels mapped to actionable user-facing messages.
var (
	ErrToolMissing = errors.New("required tool missing")
	ErrStaleStream = errors.New("stream URL expired or produced no audio")
	ErrFFmpeg      = errors.New("ffmpeg playback failed")
	ErrNetwork     = errors.New("network error")
	ErrUnavailable = errors.New("track unavailable")
	ErrCanceled    = errors.New("playback canceled")
)

// Error wraps an underlying failure with a taxonomy kind and user-facing text.
type Error struct {
	Kind        error
	Cause       error
	UserMessage string
}

func (e *Error) Error() string {
	return e.UserMessage
}

func (e *Error) Unwrap() []error {
	return []error{e.Kind, e.Cause}
}

// ClassifyError maps a raw resolver, opener, or sink error into a PlaybackError
// with a stable sentinel kind and an actionable user-facing message.
func ClassifyError(err error) *Error {
	if err == nil {
		return nil
	}
	var existing *Error
	if errors.As(err, &existing) {
		return existing
	}

	if errors.Is(err, context.Canceled) {
		return &Error{
			Kind:        ErrCanceled,
			Cause:       err,
			UserMessage: "Playback canceled.",
		}
	}

	if errors.Is(err, exec.ErrNotFound) {
		return &Error{
			Kind:        ErrToolMissing,
			Cause:       err,
			UserMessage: fmt.Sprintf("Required tool not found (%v). Install yt-dlp and ffmpeg on PATH.", err),
		}
	}

	if errors.Is(err, audio.ErrNoAudio) {
		return &Error{
			Kind:        ErrStaleStream,
			Cause:       err,
			UserMessage: "Stream URL expired or produced no audio. Try playing again.",
		}
	}

	lower := strings.ToLower(err.Error())

	var procErr *audio.ProcessError
	if errors.As(err, &procErr) {
		if looksLikeRateLimit(lower) {
			return &Error{
				Kind:        ErrNetwork,
				Cause:       err,
				UserMessage: "YouTube rate-limited the audio stream (HTTP 429). Wait a moment and retry.",
			}
		}
		if looksLikeNetworkError(lower) {
			return &Error{
				Kind:        ErrNetwork,
				Cause:       err,
				UserMessage: fmt.Sprintf("Network error while streaming audio (%v). Check your connection and retry.", err),
			}
		}
		return &Error{
			Kind:        ErrFFmpeg,
			Cause:       err,
			UserMessage: fmt.Sprintf("Audio decoder error (%v).", err),
		}
	}

	if looksLikeRateLimit(lower) {
		return &Error{
			Kind:        ErrNetwork,
			Cause:       err,
			UserMessage: "YouTube rate-limited the request (HTTP 429). Wait a moment and try again.",
		}
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{
			Kind:        ErrNetwork,
			Cause:       err,
			UserMessage: "Request timed out while resolving or streaming audio. Check your connection and retry.",
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) || looksLikeNetworkError(lower) {
		return &Error{
			Kind:        ErrNetwork,
			Cause:       err,
			UserMessage: fmt.Sprintf("Network error (%v). Check your connection or try again.", err),
		}
	}

	if looksLikeBotCheck(lower) {
		return &Error{
			Kind:        ErrUnavailable,
			Cause:       err,
			UserMessage: "YouTube requested bot verification for this track. Try updating yt-dlp or playing another track.",
		}
	}

	if looksLikeAgeRestricted(lower) {
		return &Error{
			Kind:        ErrUnavailable,
			Cause:       err,
			UserMessage: "Track is age-restricted and requires sign-in (unsupported in unauthenticated mode).",
		}
	}

	if looksLikeMembersOnly(lower) {
		return &Error{
			Kind:        ErrUnavailable,
			Cause:       err,
			UserMessage: "Track is members-only and cannot be played without channel membership.",
		}
	}

	if looksLikeUnavailableError(lower) {
		return &Error{
			Kind:        ErrUnavailable,
			Cause:       err,
			UserMessage: fmt.Sprintf("Track is unavailable (%v). If yt-dlp is outdated, try updating it.", err),
		}
	}

	if strings.Contains(lower, "ffmpeg") {
		return &Error{
			Kind:        ErrFFmpeg,
			Cause:       err,
			UserMessage: fmt.Sprintf("Audio decoder error (%v).", err),
		}
	}

	return &Error{
		Kind:        ErrUnavailable,
		Cause:       err,
		UserMessage: fmt.Sprintf("Could not play track (%v). Try updating yt-dlp if this persists.", err),
	}
}

func looksLikeNetworkError(lower string) bool {
	patterns := []string{
		"timed out",
		"timeout",
		"temporary failure in name resolution",
		"no such host",
		"network is unreachable",
		"connection refused",
		"connection reset",
		"unable to download",
		"http error 5",
		"http error 429",
		"too many requests",
		"tls",
		"ssl",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func looksLikeRateLimit(lower string) bool {
	return containsAny(lower,
		"http error 429",
		"too many requests",
		"rate limit",
		"rate-limit",
	)
}

func looksLikeBotCheck(lower string) bool {
	return containsAny(lower,
		"sign in to confirm you're not a bot",
		"sign in to confirm you\u2019re not a bot",
		"not a bot",
	)
}

func looksLikeAgeRestricted(lower string) bool {
	return containsAny(lower,
		"confirm your age",
		"age-restricted",
		"age restricted",
	)
}

func looksLikeMembersOnly(lower string) bool {
	return containsAny(lower,
		"members-only",
		"members only",
		"join this channel",
	)
}

func looksLikeUnavailableError(lower string) bool {
	return containsAny(lower,
		"video unavailable",
		"private video",
		"sign in to confirm",
		"copyright",
		"not available",
		"unsupported url",
		"could not resolve stream",
	)
}

func containsAny(s string, patterns ...string) bool {
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}
