// Package mpris exposes ytm-wmp playback state and controls over the Linux
// MPRIS D-Bus Interface Specification v2.2 (org.mpris.MediaPlayer2).
package mpris

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"

	"github.com/rroy-ar/ytm-wmp/internal/model"
	"github.com/rroy-ar/ytm-wmp/internal/playback"
)

// Controller is the subset of playback.Controller used by the MPRIS server.
type Controller interface {
	Send(cmd playback.Command) bool
	Snapshot() playback.Snapshot
	Subscribe() (<-chan struct{}, func())
}

// Option configures a Server.
type Option func(*Server)

// WithQuit registers the callback invoked by org.mpris.MediaPlayer2.Quit.
func WithQuit(fn func()) Option {
	return func(s *Server) {
		s.onQuit = fn
	}
}

// WithIdentity overrides the MPRIS Identity property (default: "ytm-wmp").
func WithIdentity(identity string) Option {
	return func(s *Server) {
		if identity != "" {
			s.identity = identity
		}
	}
}

// WithDesktopEntry overrides the MPRIS DesktopEntry property (default: "ytm-wmp").
func WithDesktopEntry(entry string) Option {
	return func(s *Server) {
		if entry != "" {
			s.desktopEntry = entry
		}
	}
}

// WithBusName overrides the primary D-Bus well-known name.
func WithBusName(name string) Option {
	return func(s *Server) {
		if name != "" {
			s.baseBusName = name
		}
	}
}

// WithConn injects an existing D-Bus connection (e.g. a private test bus).
// When provided, Close does not close the underlying connection.
func WithConn(conn *dbus.Conn) Option {
	return func(s *Server) {
		s.conn = conn
	}
}

// Server publishes ytm-wmp on the D-Bus session bus and translates MPRIS
// method calls and property updates into playback.Command messages.
type Server struct {
	ctrl         Controller
	conn         *dbus.Conn
	ownsConn     bool
	baseBusName  string
	busName      string
	identity     string
	desktopEntry string
	onQuit       func()

	stopCh    chan struct{}
	doneCh    chan struct{}
	unsub     func()
	closeOnce sync.Once
}

// New connects to the session D-Bus (unless WithConn was supplied), exports
// the MPRIS2 interfaces, acquires a well-known bus name, and starts emitting
// property-change and seek signals.
func New(ctrl Controller, opts ...Option) (*Server, error) {
	if ctrl == nil {
		return nil, errors.New("mpris: controller is required")
	}
	s := &Server{
		ctrl:         ctrl,
		baseBusName:  DefaultBusName,
		identity:     "ytm-wmp",
		desktopEntry: "ytm-wmp",
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}

	if s.conn == nil {
		conn, err := dbus.ConnectSessionBus()
		if err != nil {
			return nil, fmt.Errorf("connect session bus: %w", err)
		}
		s.conn = conn
		s.ownsConn = true
	}

	if err := s.export(); err != nil {
		s.cleanupConn()
		return nil, err
	}

	busName, err := s.requestBusName()
	if err != nil {
		s.unexport()
		s.cleanupConn()
		return nil, err
	}
	s.busName = busName

	subCh, unsub := s.ctrl.Subscribe()
	s.unsub = unsub
	go s.watchLoop(subCh)

	slog.Info("mpris server started", "bus_name", s.busName)
	return s, nil
}

// BusName returns the D-Bus well-known name acquired by this server.
func (s *Server) BusName() string {
	return s.busName
}

// Close stops signal emission, releases the D-Bus name, unexports objects, and
// closes the session bus connection (if owned by Server).
func (s *Server) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		close(s.stopCh)
		if s.doneCh != nil {
			<-s.doneCh
		}
		if s.unsub != nil {
			s.unsub()
		}
		if s.conn != nil {
			if s.busName != "" {
				_, _ = s.conn.ReleaseName(s.busName)
			}
			s.unexport()
			if s.ownsConn {
				closeErr = s.conn.Close()
			}
		}
	})
	return closeErr
}

func (s *Server) export() error {
	if err := s.conn.Export(&rootObject{server: s}, ObjectPath, rootInterface); err != nil {
		return fmt.Errorf("export %s: %w", rootInterface, err)
	}
	if err := s.conn.ExportWithMap(&playerObject{server: s}, map[string]string{
		"SeekOffset": "Seek",
	}, ObjectPath, playerInterface); err != nil {
		return fmt.Errorf("export %s: %w", playerInterface, err)
	}
	if err := s.conn.Export(&propsObject{server: s}, ObjectPath, propertiesInterface); err != nil {
		return fmt.Errorf("export %s: %w", propertiesInterface, err)
	}
	if err := s.conn.Export(introspect.NewIntrospectable(introspectNode()), ObjectPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		return fmt.Errorf("export introspectable: %w", err)
	}
	return nil
}

func (s *Server) unexport() {
	if s.conn == nil {
		return
	}
	_ = s.conn.Export(nil, ObjectPath, rootInterface)
	_ = s.conn.Export(nil, ObjectPath, playerInterface)
	_ = s.conn.Export(nil, ObjectPath, propertiesInterface)
	_ = s.conn.Export(nil, ObjectPath, "org.freedesktop.DBus.Introspectable")
}

func (s *Server) cleanupConn() {
	if s.ownsConn && s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
}

func (s *Server) requestBusName() (string, error) {
	reply, err := s.conn.RequestName(s.baseBusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return "", fmt.Errorf("request bus name %q: %w", s.baseBusName, err)
	}
	if reply == dbus.RequestNameReplyPrimaryOwner {
		return s.baseBusName, nil
	}
	instanceName := fmt.Sprintf("%s.instance%d", s.baseBusName, os.Getpid())
	reply, err = s.conn.RequestName(instanceName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return "", fmt.Errorf("request instance bus name %q: %w", instanceName, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return "", fmt.Errorf("bus name %q already taken", instanceName)
	}
	return instanceName, nil
}

func (s *Server) watchLoop(subCh <-chan struct{}) {
	defer close(s.doneCh)
	prev := s.ctrl.Snapshot()

	for {
		select {
		case <-s.stopCh:
			return
		case _, ok := <-subCh:
			if !ok {
				return
			}
			cur := s.ctrl.Snapshot()
			changed, seeked := diffPlayerProperties(prev, cur)
			if len(changed) > 0 {
				if err := s.conn.Emit(
					ObjectPath,
					propertiesInterface+".PropertiesChanged",
					playerInterface,
					changed,
					[]string{},
				); err != nil {
					slog.Debug("mpris emit PropertiesChanged failed", "err", err)
				}
			}
			if seeked {
				if err := s.conn.Emit(
					ObjectPath,
					playerInterface+".Seeked",
					cur.Position.Microseconds(),
				); err != nil {
					slog.Debug("mpris emit Seeked failed", "err", err)
				}
			}
			prev = cur
		}
	}
}

type rootObject struct {
	server *Server
}

func (r *rootObject) Raise() *dbus.Error {
	return nil
}

func (r *rootObject) Quit() *dbus.Error {
	if r.server.onQuit != nil {
		r.server.onQuit()
	}
	return nil
}

type playerObject struct {
	server *Server
}

func (p *playerObject) Next() *dbus.Error {
	p.server.ctrl.Send(playback.Next{})
	return nil
}

func (p *playerObject) Previous() *dbus.Error {
	p.server.ctrl.Send(playback.Prev{})
	return nil
}

func (p *playerObject) Pause() *dbus.Error {
	p.server.ctrl.Send(playback.Pause{})
	return nil
}

func (p *playerObject) PlayPause() *dbus.Error {
	snap := p.server.ctrl.Snapshot()
	switch snap.State {
	case playback.StatePlaying, playback.StateResolving, playback.StateSeeking:
		p.server.ctrl.Send(playback.Pause{})
	case playback.StatePaused:
		p.server.ctrl.Send(playback.Resume{})
	default:
		p.server.ctrl.Send(playback.Play{})
	}
	return nil
}

func (p *playerObject) Stop() *dbus.Error {
	p.server.ctrl.Send(playback.Stop{})
	return nil
}

func (p *playerObject) Play() *dbus.Error {
	p.server.ctrl.Send(playback.Play{})
	return nil
}

// SeekOffset implements org.mpris.MediaPlayer2.Player.Seek (mapped via
// ExportWithMap to avoid go vet's io.Seeker signature check).
func (p *playerObject) SeekOffset(offsetUs int64) *dbus.Error {
	snap := p.server.ctrl.Snapshot()
	if !canSeek(snap) {
		return nil
	}
	target := snap.Position + time.Duration(offsetUs)*time.Microsecond
	if target < 0 {
		target = 0
	}
	if snap.Duration > 0 && target > snap.Duration {
		p.server.ctrl.Send(playback.Next{})
		return nil
	}
	p.server.ctrl.Send(playback.Seek{Position: target})
	return nil
}

func (p *playerObject) SetPosition(trackID dbus.ObjectPath, positionUs int64) *dbus.Error {
	snap := p.server.ctrl.Snapshot()
	if !canSeek(snap) {
		return nil
	}
	if trackID != trackObjectPath(snap.Track.ID) {
		return nil
	}
	if positionUs < 0 {
		return nil
	}
	target := time.Duration(positionUs) * time.Microsecond
	if snap.Duration > 0 && target > snap.Duration {
		return nil
	}
	p.server.ctrl.Send(playback.Seek{Position: target})
	return nil
}

func (p *playerObject) OpenUri(uri string) *dbus.Error {
	vid := model.VideoID(uri)
	if vid == "" {
		return errInvalidArgs
	}
	p.server.ctrl.Send(playback.SetQueue{
		Tracks:   []model.Track{{ID: vid}},
		Index:    0,
		AutoPlay: true,
	})
	return nil
}
