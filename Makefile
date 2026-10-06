.PHONY: build test lint demo install video video-server icon release

build:
	go build -o mmt .

test:
	go test -race ./...

lint:
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...
	@test -z "$$(gofmt -l .)" || (gofmt -l .; echo "run gofmt -w ."; exit 1)

# Start the mock server and mmt against it; no real Mattermost needed.
demo: build
	@mkdir -p dist && go build -o dist/mockserver ./tools/mockserver
	@dist/mockserver -addr 127.0.0.1:8065 & \
	pid=$$!; trap "kill $$pid" EXIT; sleep 1; \
	MMT_URL=http://127.0.0.1:8065 MMT_TOKEN=demo MMT_LANG=en ./mmt --here

install:
	go install .

# Screen recording: `make video` in an iTerm2 tab is enough. For the key
# controls (mentions, calls, DMs on cue), run `make video-server` in another
# terminal first, off camera; `make video` then uses it instead of starting
# its own. --here keeps mmt in that tab, so it stays on the mock server.
video-server:
	@mkdir -p dist && go build -o dist/mockserver ./tools/mockserver
	@dist/mockserver -scenario video -addr 127.0.0.1:8065

video: build
	@mkdir -p dist && go build -o dist/mockserver ./tools/mockserver
	@if nc -z 127.0.0.1 8065 2>/dev/null; then \
		MMT_URL=http://127.0.0.1:8065 MMT_TOKEN=demo MMT_LANG=en ./mmt --here; \
	else \
		dist/mockserver -scenario video -addr 127.0.0.1:8065 </dev/null >dist/video-server.log 2>&1 & \
		pid=$$!; trap "kill $$pid" EXIT; sleep 1; \
		MMT_URL=http://127.0.0.1:8065 MMT_TOKEN=demo MMT_LANG=en ./mmt --here; \
	fi

# Rebuild the app icon from docs/assets/icon.svg (renders with Google Chrome).
icon:
	@tmp=$$(mktemp -d) && \
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --disable-gpu \
		--hide-scrollbars --default-background-color=00000000 --window-size=1024,1024 \
		--screenshot=$$tmp/icon.png "file://$(CURDIR)/docs/assets/icon.svg" 2>/dev/null && \
	mkdir $$tmp/mmt.iconset && \
	for s in 16 32 128 256 512; do \
		sips -z $$s $$s $$tmp/icon.png --out $$tmp/mmt.iconset/icon_$${s}x$${s}.png >/dev/null; \
		sips -z $$((s*2)) $$((s*2)) $$tmp/icon.png --out $$tmp/mmt.iconset/icon_$${s}x$${s}@2x.png >/dev/null; \
	done && \
	iconutil -c icns $$tmp/mmt.iconset -o internal/background/mmt.icns && \
	sips -z 256 256 $$tmp/icon.png --out docs/assets/icon.png >/dev/null && \
	echo "wrote internal/background/mmt.icns and docs/assets/icon.png"

# Release archives in dist/: a universal macOS binary (needs a Mac, for cgo),
# Linux and Windows builds. make release VERSION=v0.2.0
VERSION ?= v0.1.0-beta.$(shell date +%Y%m%d)
LDFLAGS := -s -w -X main.version=$(VERSION)

release:
	@rm -rf dist/release && mkdir -p dist/release
	@for d in macos linux-amd64 linux-arm64; do mkdir -p dist/release/mmt-$$d; \
		cp README.md LICENSE dist/release/mmt-$$d/; cp scripts/install.sh dist/release/mmt-$$d/; done
	@for a in amd64 arm64; do mkdir -p dist/release/mmt-windows-$$a; \
		cp README.md LICENSE scripts/install.ps1 dist/release/mmt-windows-$$a/; done
	CGO_ENABLED=1 GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/release/mmt-arm64 .
	CGO_ENABLED=1 GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/release/mmt-amd64 .
	lipo -create -output dist/release/mmt-macos/mmt dist/release/mmt-arm64 dist/release/mmt-amd64
	codesign --force --sign - dist/release/mmt-macos/mmt
	@rm dist/release/mmt-arm64 dist/release/mmt-amd64
	for a in amd64 arm64; do \
		CGO_ENABLED=0 GOOS=linux GOARCH=$$a go build -trimpath -ldflags "$(LDFLAGS)" -o dist/release/mmt-linux-$$a/mmt . && \
		CGO_ENABLED=0 GOOS=windows GOARCH=$$a go build -trimpath -ldflags "$(LDFLAGS)" -o dist/release/mmt-windows-$$a/mmt.exe . || exit 1; \
	done
	@cd dist/release && \
	for d in macos linux-amd64 linux-arm64; do COPYFILE_DISABLE=1 tar -czf mmt-$$d-$(VERSION).tar.gz mmt-$$d; done && \
	for a in amd64 arm64; do zip -qr mmt-windows-$$a-$(VERSION).zip mmt-windows-$$a; done && \
	shasum -a 256 *.tar.gz *.zip > SHA256SUMS && cat SHA256SUMS
