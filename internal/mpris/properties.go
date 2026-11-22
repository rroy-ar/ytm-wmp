package mpris

import (
	"fmt"
	"math"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"

	"github.com/rroy-ar/ytm-wmp/internal/model"
	"github.com/rroy-ar/ytm-wmp/internal/playback"
)

const (
	// ObjectPath is the standard MPRIS2 D-Bus object path.
	ObjectPath dbus.ObjectPath = "/org/mpris/MediaPlayer2"

	// DefaultBusName is the well-known D-Bus name for ytm-wmp.
	DefaultBusName = "org.mpris.MediaPlayer2.ytm_wmp"

	rootInterface       = "org.mpris.MediaPlayer2"
	playerInterface     = "org.mpris.MediaPlayer2.Player"
	propertiesInterface = "org.freedesktop.DBus.Properties"

	noTrackPath     dbus.ObjectPath = "/org/mpris/MediaPlayer2/TrackList/NoTrack"
	trackPathPrefix                 = "/org/mpris/MediaPlayer2/ytm_wmp/track/"
)

var (
	errUnknownInterface = dbus.NewError("org.freedesktop.DBus.Error.UnknownInterface", []any{"unknown interface"})
	errUnknownProperty  = dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty", []any{"unknown property"})
	errReadOnlyProperty = dbus.NewError("org.freedesktop.DBus.Error.PropertyReadOnly", []any{"property is read-only"})
	errInvalidArgs      = dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs", []any{"invalid arguments"})
)

func playbackStatus(state playback.State) string {
	switch state {
	case playback.StatePlaying, playback.StateResolving, playback.StateSeeking:
		return "Playing"
	case playback.StatePaused:
		return "Paused"
	default:
		return "Stopped"
	}
}

func loopStatus(mode playback.RepeatMode) string {
	switch mode {
	case playback.RepeatOne:
		return "Track"
	case playback.RepeatAll:
		return "Playlist"
	default:
		return "None"
	}
}

func parseLoopStatus(s string) (playback.RepeatMode, bool) {
	switch s {
	case "None":
		return playback.RepeatOff, true
	case "Track":
		return playback.RepeatOne, true
	case "Playlist":
		return playback.RepeatAll, true
	default:
		return playback.RepeatOff, false
	}
}

// trackObjectPath encodes a track ID into a valid D-Bus object path.
// D-Bus object path elements may only contain [A-Za-z0-9_].
func trackObjectPath(trackID string) dbus.ObjectPath {
	id := strings.TrimSpace(trackID)
	if id == "" {
		return noTrackPath
	}
	var b strings.Builder
	b.Grow(len(trackPathPrefix) + len(id) + 8)
	b.WriteString(trackPathPrefix)
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9'):
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "_%02x", c)
		}
	}
	return dbus.ObjectPath(b.String())
}

func trackArtURL(track model.Track) string {
	if u := strings.TrimSpace(track.ThumbnailURL); u != "" {
		return u
	}
	return model.DefaultThumbnailURL(track.ID)
}

func trackMetadata(snap playback.Snapshot) map[string]dbus.Variant {
	if len(snap.Queue) == 0 && strings.TrimSpace(snap.Track.ID) == "" && strings.TrimSpace(snap.Track.Title) == "" {
		return map[string]dbus.Variant{}
	}
	meta := make(map[string]dbus.Variant, 7)
	meta["mpris:trackid"] = dbus.MakeVariant(trackObjectPath(snap.Track.ID))

	dur := snap.Duration
	if dur <= 0 {
		dur = snap.Track.Duration
	}
	if dur > 0 {
		meta["mpris:length"] = dbus.MakeVariant(dur.Microseconds())
	}

	if art := trackArtURL(snap.Track); art != "" {
		meta["mpris:artUrl"] = dbus.MakeVariant(art)
	}

	title := strings.TrimSpace(snap.Track.Title)
	if title == "" {
		title = strings.TrimSpace(snap.Track.ID)
	}
	if title != "" {
		meta["xesam:title"] = dbus.MakeVariant(title)
	}

	if artist := strings.TrimSpace(snap.Track.Artist); artist != "" {
		meta["xesam:artist"] = dbus.MakeVariant([]string{artist})
	}

	if album := strings.TrimSpace(snap.Track.Album); album != "" {
		meta["xesam:album"] = dbus.MakeVariant(album)
	}

	if vid := model.VideoID(snap.Track.ID); vid != "" {
		meta["xesam:url"] = dbus.MakeVariant("https://music.youtube.com/watch?v=" + vid)
	}
	return meta
}

func canGoNext(snap playback.Snapshot) bool {
	if len(snap.Queue) == 0 {
		return false
	}
	if snap.Autoplay || snap.Repeat != playback.RepeatOff || snap.Shuffle {
		return true
	}
	return snap.Index >= 0 && snap.Index < len(snap.Queue)-1
}

func canGoPrevious(snap playback.Snapshot) bool {
	return len(snap.Queue) > 0
}

func canPlay(snap playback.Snapshot) bool {
	return len(snap.Queue) > 0
}

func canPause(snap playback.Snapshot) bool {
	return len(snap.Queue) > 0
}

func canSeek(snap playback.Snapshot) bool {
	if snap.Duration <= 0 {
		return false
	}
	switch snap.State {
	case playback.StatePlaying, playback.StatePaused, playback.StateSeeking:
		return true
	default:
		return false
	}
}

func clampVolume(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func metadataEqual(a, b playback.Snapshot) bool {
	aEmpty := len(a.Queue) == 0 && strings.TrimSpace(a.Track.ID) == "" && strings.TrimSpace(a.Track.Title) == ""
	bEmpty := len(b.Queue) == 0 && strings.TrimSpace(b.Track.ID) == "" && strings.TrimSpace(b.Track.Title) == ""
	if aEmpty != bEmpty {
		return false
	}
	if aEmpty {
		return true
	}
	aDur := a.Duration
	if aDur <= 0 {
		aDur = a.Track.Duration
	}
	bDur := b.Duration
	if bDur <= 0 {
		bDur = b.Track.Duration
	}
	return a.Track.ID == b.Track.ID &&
		a.Track.Title == b.Track.Title &&
		a.Track.Artist == b.Track.Artist &&
		a.Track.Album == b.Track.Album &&
		aDur == bDur &&
		trackArtURL(a.Track) == trackArtURL(b.Track)
}

// diffPlayerProperties returns the MPRIS Player properties that changed between
// prev and cur, plus whether a Seeked signal should be emitted. Position is
// intentionally excluded per the MPRIS2 specification.
func diffPlayerProperties(prev, cur playback.Snapshot) (map[string]dbus.Variant, bool) {
	changed := make(map[string]dbus.Variant)

	if ps := playbackStatus(cur.State); ps != playbackStatus(prev.State) {
		changed["PlaybackStatus"] = dbus.MakeVariant(ps)
	}
	if ls := loopStatus(cur.Repeat); ls != loopStatus(prev.Repeat) {
		changed["LoopStatus"] = dbus.MakeVariant(ls)
	}
	if cur.Shuffle != prev.Shuffle {
		changed["Shuffle"] = dbus.MakeVariant(cur.Shuffle)
	}
	if math.Abs(clampVolume(cur.Volume)-clampVolume(prev.Volume)) > 1e-6 {
		changed["Volume"] = dbus.MakeVariant(clampVolume(cur.Volume))
	}
	if !metadataEqual(prev, cur) {
		changed["Metadata"] = dbus.MakeVariant(trackMetadata(cur))
	}
	if v := canGoNext(cur); v != canGoNext(prev) {
		changed["CanGoNext"] = dbus.MakeVariant(v)
	}
	if v := canGoPrevious(cur); v != canGoPrevious(prev) {
		changed["CanGoPrevious"] = dbus.MakeVariant(v)
	}
	if v := canPlay(cur); v != canPlay(prev) {
		changed["CanPlay"] = dbus.MakeVariant(v)
	}
	if v := canPause(cur); v != canPause(prev) {
		changed["CanPause"] = dbus.MakeVariant(v)
	}
	if v := canSeek(cur); v != canSeek(prev) {
		changed["CanSeek"] = dbus.MakeVariant(v)
	}

	seeked := cur.State == playback.StateSeeking && (prev.State != playback.StateSeeking || prev.Position != cur.Position)
	return changed, seeked
}

type propsObject struct {
	server *Server
}

func (p *propsObject) rootProperties() map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"CanQuit":             dbus.MakeVariant(p.server.onQuit != nil),
		"CanRaise":            dbus.MakeVariant(false),
		"HasTrackList":        dbus.MakeVariant(false),
		"Identity":            dbus.MakeVariant(p.server.identity),
		"DesktopEntry":        dbus.MakeVariant(p.server.desktopEntry),
		"SupportedUriSchemes": dbus.MakeVariant([]string{"http", "https"}),
		"SupportedMimeTypes":  dbus.MakeVariant([]string{}),
	}
}

func (p *propsObject) playerProperties(snap playback.Snapshot) map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"PlaybackStatus": dbus.MakeVariant(playbackStatus(snap.State)),
		"LoopStatus":     dbus.MakeVariant(loopStatus(snap.Repeat)),
		"Rate":           dbus.MakeVariant(1.0),
		"Shuffle":        dbus.MakeVariant(snap.Shuffle),
		"Metadata":       dbus.MakeVariant(trackMetadata(snap)),
		"Volume":         dbus.MakeVariant(clampVolume(snap.Volume)),
		"Position":       dbus.MakeVariant(snap.Position.Microseconds()),
		"MinimumRate":    dbus.MakeVariant(1.0),
		"MaximumRate":    dbus.MakeVariant(1.0),
		"CanGoNext":      dbus.MakeVariant(canGoNext(snap)),
		"CanGoPrevious":  dbus.MakeVariant(canGoPrevious(snap)),
		"CanPlay":        dbus.MakeVariant(canPlay(snap)),
		"CanPause":       dbus.MakeVariant(canPause(snap)),
		"CanSeek":        dbus.MakeVariant(canSeek(snap)),
		"CanControl":     dbus.MakeVariant(true),
	}
}

func (p *propsObject) Get(iface, property string) (dbus.Variant, *dbus.Error) {
	switch iface {
	case rootInterface:
		if v, ok := p.rootProperties()[property]; ok {
			return v, nil
		}
		return dbus.Variant{}, errUnknownProperty
	case playerInterface:
		snap := p.server.ctrl.Snapshot()
		if v, ok := p.playerProperties(snap)[property]; ok {
			return v, nil
		}
		return dbus.Variant{}, errUnknownProperty
	default:
		return dbus.Variant{}, errUnknownInterface
	}
}

func (p *propsObject) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	switch iface {
	case rootInterface:
		return p.rootProperties(), nil
	case playerInterface:
		return p.playerProperties(p.server.ctrl.Snapshot()), nil
	default:
		return nil, errUnknownInterface
	}
}

func (p *propsObject) Set(iface, property string, value dbus.Variant) *dbus.Error {
	if iface == rootInterface {
		if _, ok := p.rootProperties()[property]; ok {
			return errReadOnlyProperty
		}
		return errUnknownProperty
	}
	if iface != playerInterface {
		return errUnknownInterface
	}

	switch property {
	case "LoopStatus":
		s, ok := value.Value().(string)
		if !ok {
			return errInvalidArgs
		}
		mode, ok := parseLoopStatus(s)
		if !ok {
			return errInvalidArgs
		}
		p.server.ctrl.Send(playback.SetRepeat{Mode: mode})
		return nil

	case "Shuffle":
		b, ok := value.Value().(bool)
		if !ok {
			return errInvalidArgs
		}
		p.server.ctrl.Send(playback.SetShuffle{Enabled: b})
		return nil

	case "Volume":
		v, ok := value.Value().(float64)
		if !ok || math.IsNaN(v) {
			return errInvalidArgs
		}
		p.server.ctrl.Send(playback.SetVolume{Volume: clampVolume(v)})
		return nil

	case "Rate":
		v, ok := value.Value().(float64)
		if !ok || math.IsNaN(v) || v <= 0 {
			return errInvalidArgs
		}
		// Only 1.0x playback rate is supported (MinimumRate == MaximumRate == 1.0).
		return nil

	case "PlaybackStatus", "Metadata", "Position", "MinimumRate", "MaximumRate",
		"CanGoNext", "CanGoPrevious", "CanPlay", "CanPause", "CanSeek", "CanControl":
		return errReadOnlyProperty

	default:
		return errUnknownProperty
	}
}

func introspectNode() *introspect.Node {
	return &introspect.Node{
		Name: string(ObjectPath),
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			{
				Name: propertiesInterface,
				Methods: []introspect.Method{
					{
						Name: "Get",
						Args: []introspect.Arg{
							{Name: "interface", Type: "s", Direction: "in"},
							{Name: "property", Type: "s", Direction: "in"},
							{Name: "value", Type: "v", Direction: "out"},
						},
					},
					{
						Name: "GetAll",
						Args: []introspect.Arg{
							{Name: "interface", Type: "s", Direction: "in"},
							{Name: "props", Type: "a{sv}", Direction: "out"},
						},
					},
					{
						Name: "Set",
						Args: []introspect.Arg{
							{Name: "interface", Type: "s", Direction: "in"},
							{Name: "property", Type: "s", Direction: "in"},
							{Name: "value", Type: "v", Direction: "in"},
						},
					},
				},
				Signals: []introspect.Signal{
					{
						Name: "PropertiesChanged",
						Args: []introspect.Arg{
							{Name: "interface", Type: "s"},
							{Name: "changed_properties", Type: "a{sv}"},
							{Name: "invalidated_properties", Type: "as"},
						},
					},
				},
			},
			{
				Name: rootInterface,
				Methods: []introspect.Method{
					{Name: "Raise"},
					{Name: "Quit"},
				},
				Properties: []introspect.Property{
					{Name: "CanQuit", Type: "b", Access: "read"},
					{Name: "CanRaise", Type: "b", Access: "read"},
					{Name: "HasTrackList", Type: "b", Access: "read"},
					{Name: "Identity", Type: "s", Access: "read"},
					{Name: "DesktopEntry", Type: "s", Access: "read"},
					{Name: "SupportedUriSchemes", Type: "as", Access: "read"},
					{Name: "SupportedMimeTypes", Type: "as", Access: "read"},
				},
			},
			{
				Name: playerInterface,
				Methods: []introspect.Method{
					{Name: "Next"},
					{Name: "Previous"},
					{Name: "Pause"},
					{Name: "PlayPause"},
					{Name: "Stop"},
					{Name: "Play"},
					{
						Name: "Seek",
						Args: []introspect.Arg{
							{Name: "Offset", Type: "x", Direction: "in"},
						},
					},
					{
						Name: "SetPosition",
						Args: []introspect.Arg{
							{Name: "TrackId", Type: "o", Direction: "in"},
							{Name: "Position", Type: "x", Direction: "in"},
						},
					},
					{
						Name: "OpenUri",
						Args: []introspect.Arg{
							{Name: "Uri", Type: "s", Direction: "in"},
						},
					},
				},
				Signals: []introspect.Signal{
					{
						Name: "Seeked",
						Args: []introspect.Arg{
							{Name: "Position", Type: "x"},
						},
					},
				},
				Properties: []introspect.Property{
					{Name: "PlaybackStatus", Type: "s", Access: "read"},
					{Name: "LoopStatus", Type: "s", Access: "readwrite"},
					{Name: "Rate", Type: "d", Access: "readwrite"},
					{Name: "Shuffle", Type: "b", Access: "readwrite"},
					{Name: "Metadata", Type: "a{sv}", Access: "read"},
					{Name: "Volume", Type: "d", Access: "readwrite"},
					{Name: "Position", Type: "x", Access: "read"},
					{Name: "MinimumRate", Type: "d", Access: "read"},
					{Name: "MaximumRate", Type: "d", Access: "read"},
					{Name: "CanGoNext", Type: "b", Access: "read"},
					{Name: "CanGoPrevious", Type: "b", Access: "read"},
					{Name: "CanPlay", Type: "b", Access: "read"},
					{Name: "CanPause", Type: "b", Access: "read"},
					{Name: "CanSeek", Type: "b", Access: "read"},
					{Name: "CanControl", Type: "b", Access: "read"},
				},
			},
		},
	}
}
