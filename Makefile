PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
DATADIR ?= $(PREFIX)/share
DESTDIR ?=

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build test check install uninstall clean

all: build

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o ytm-wmp ./cmd/ytm-wmp
	go build -trimpath -ldflags "$(LDFLAGS)" -o ytmctl ./cmd/ytmctl

test:
	go test -race ./...

check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "Files not formatted with gofmt:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	go vet ./...
	go build ./...
	go test -race ./...

install: build
	install -Dm755 ytm-wmp "$(DESTDIR)$(BINDIR)/ytm-wmp"
	install -Dm755 ytmctl "$(DESTDIR)$(BINDIR)/ytmctl"
	install -Dm644 packaging/ytm-wmp.desktop "$(DESTDIR)$(DATADIR)/applications/ytm-wmp.desktop"
	install -Dm644 packaging/ytm-wmp.svg "$(DESTDIR)$(DATADIR)/icons/hicolor/scalable/apps/ytm-wmp.svg"
	@if [ -z "$(DESTDIR)" ]; then \
		command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database -q "$(DATADIR)/applications" || true; \
		command -v gtk-update-icon-cache >/dev/null 2>&1 && gtk-update-icon-cache -q -t "$(DATADIR)/icons/hicolor" || true; \
	fi

uninstall:
	rm -f "$(DESTDIR)$(BINDIR)/ytm-wmp" \
		"$(DESTDIR)$(BINDIR)/ytmctl" \
		"$(DESTDIR)$(DATADIR)/applications/ytm-wmp.desktop" \
		"$(DESTDIR)$(DATADIR)/icons/hicolor/scalable/apps/ytm-wmp.svg"

clean:
	rm -f ytm-wmp ytmctl
