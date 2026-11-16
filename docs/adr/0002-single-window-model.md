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
- Phase 1 starts with a fixed 275x116 main window (and Phase 3A stacks the
  275x232 catalog browser beneath it at 275x348);
- Phase 4B uses a **docked/stacked Winamp rack** (`Main 275x116`, toggleable
  `EQ 275x116`, toggleable `Playlist/Browser 275x232`) inside the single OS
  window, resizing the OS window only on discrete window show/hide toggles
  (`EQ` / `PL` buttons) rather than dynamically morphing the OS window bounding
  box during mouse drags on X11/Wayland;
- arbitrary free-floating window drags, shade mode, double-size, and region
  masks are Phase 6 (or later), not v1.

## Consequences

- Transparent gaps between windows are impossible; the canvas is a clean,
  predictable stack of the currently open rack panels.
- Avoiding continuous OS window resizes during pointer drags avoids X11/Wayland
  compositor flicker, cursor warp, and WM resize lag.
- Alt-tab, focus, and window-manager behavior are those of one window, not three.
- Show/hide state, focus/active title bars, and per-panel hit testing remain
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
