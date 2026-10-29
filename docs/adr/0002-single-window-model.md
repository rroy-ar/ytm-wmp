# ADR 0002: One OS window with internal Winamp-style windows

- Status: Accepted
- Date: 2026-10-25
- Deciders: project owner
- Related: plan decision D2, Phase 4B

## Context

Classic Winamp uses separate always-on-top windows for the main player,
playlist, and equalizer, with independent positions and window shading. The
project wants that visual and interaction model.

Ebitengine owns exactly one OS window and one event loop. Creating a second
Ebitengine window in the same process is not supported. A second UI stack
(native toolkit or platform window APIs) would duplicate input, focus, drawing,
and skin rendering.

## Decision

Use one OS window that contains draggable internal windows:

- main, playlist, equalizer, browser/search, and settings are rectangles in
  logical 1x coordinates;
- the title bar drags a window; clicking focuses it and raises its z-order;
- title bars render active/inactive variants from the skin;
- Phase 1 starts with a fixed 275x116 main window;
- Phase 4B grows the OS window to the union of visible internal windows and
  translates draw/mouse coordinates only when the layout changes;
- shade mode, double-size, and region masks are Phase 6 (or later), not v1.

## Consequences

- Transparent gaps between windows are impossible; the canvas is the bounding
  rectangle of the visible windows.
- Alt-tab, focus, and window-manager behavior are those of one window, not three.
- Snapping, z-order, and "show/hide" must be implemented by the app, but that is
  small immediate-mode code compared with a second UI stack.
- Screenshots and tests can render the whole player deterministically.

## Alternatives considered

- **Multiple OS windows via a native toolkit.** Rejects the skin renderer and
  complicates input/focus; too much work for v1.
- **A transparent, click-through canvas on top of native windows.** Platform
  specific and fragile; explicitly deferred.
- **Tabs inside one window instead of floating internal windows.** Loses the
  distinctive Winamp layout and is not materially simpler once drag/z-order
  exists.
