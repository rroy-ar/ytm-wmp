#!/bin/sh
set -eu

usage() {
	cat <<'EOF'
Usage: install.sh [--uninstall]

Environment variables:
  PREFIX   Install prefix (default: ~/.local for non-root, /usr/local for root)
  DESTDIR  Staging directory prepended to PREFIX (optional)
EOF
}

UNINSTALL=0
for arg in "$@"; do
	case "$arg" in
		--uninstall)
			UNINSTALL=1
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			echo "install.sh: unknown option: $arg" >&2
			usage >&2
			exit 1
			;;
	esac
done

if [ -z "${PREFIX:-}" ]; then
	if [ "$(id -u)" -eq 0 ]; then
		PREFIX="/usr/local"
	else
		PREFIX="${HOME}/.local"
	fi
fi

case "$PREFIX" in
	"~")
		PREFIX="${HOME}"
		;;
	"~/"*)
		PREFIX="${HOME}/${PREFIX#\~/}"
		;;
esac

DESTDIR="${DESTDIR:-}"
BINDIR="${DESTDIR}${PREFIX}/bin"
APPDIR="${DESTDIR}${PREFIX}/share/applications"
ICONDIR="${DESTDIR}${PREFIX}/share/icons/hicolor/scalable/apps"

if [ "$UNINSTALL" -eq 1 ]; then
	rm -f "${BINDIR}/ytm-wmp" "${BINDIR}/ytmctl"
	rm -f "${APPDIR}/ytm-wmp.desktop"
	rm -f "${ICONDIR}/ytm-wmp.svg"
	echo "Uninstalled ytm-wmp from ${DESTDIR}${PREFIX}"
	exit 0
fi

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"

# Support running either from an extracted release tarball root or from repo packaging/.
if [ -f "${SCRIPT_DIR}/ytm-wmp" ] && [ -f "${SCRIPT_DIR}/packaging/ytm-wmp.desktop" ]; then
	SRC_BIN="${SCRIPT_DIR}"
	SRC_PKG="${SCRIPT_DIR}/packaging"
elif [ -f "${SCRIPT_DIR}/../ytm-wmp" ] && [ -f "${SCRIPT_DIR}/ytm-wmp.desktop" ]; then
	SRC_BIN="${SCRIPT_DIR}/.."
	SRC_PKG="${SCRIPT_DIR}"
else
	echo "install.sh: built ytm-wmp binary not found; run 'make build' first or run from an extracted release archive." >&2
	exit 1
fi

install -Dm755 "${SRC_BIN}/ytm-wmp" "${BINDIR}/ytm-wmp"
if [ -f "${SRC_BIN}/ytmctl" ]; then
	install -Dm755 "${SRC_BIN}/ytmctl" "${BINDIR}/ytmctl"
fi
install -Dm644 "${SRC_PKG}/ytm-wmp.desktop" "${APPDIR}/ytm-wmp.desktop"
install -Dm644 "${SRC_PKG}/ytm-wmp.svg" "${ICONDIR}/ytm-wmp.svg"

if [ -z "$DESTDIR" ]; then
	if command -v update-desktop-database >/dev/null 2>&1; then
		update-desktop-database -q "$APPDIR" >/dev/null 2>&1 || true
	fi
	if command -v gtk-update-icon-cache >/dev/null 2>&1; then
		gtk-update-icon-cache -q -t "${PREFIX}/share/icons/hicolor" >/dev/null 2>&1 || true
	fi
fi

echo "Installed ytm-wmp to ${PREFIX}"
