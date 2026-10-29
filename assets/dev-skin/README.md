# Development skin

This is an original, deliberately plain skin used when `-skin` is not supplied.
It exists so the player works out of the box without bundling a copyrighted
Winamp skin.

The loose BMPs use the classic sheet dimensions and coordinates:

| File | Size | Purpose |
|------|------|---------|
| `main.bmp` | 275x116 | main-window background |
| `titlebar.bmp` | 312x87 | active and inactive title bars |
| `cbuttons.bmp` | 136x36 | transport buttons, normal and pressed |

Only the Phase 1 sprites are required right now; the rest of each sheet is
painted so later phases can add the full main window without regenerating the
art. The palette and layout are original to this project. Do not add downloaded
Winamp skins here; keep those in a gitignored local folder.
