.PHONY: build test lint demo install video video-server icon

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

# Rebuild the app icon from docs/icon.svg (renders with Google Chrome).
icon:
	@tmp=$$(mktemp -d) && \
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --disable-gpu \
		--hide-scrollbars --default-background-color=00000000 --window-size=1024,1024 \
		--screenshot=$$tmp/icon.png "file://$(CURDIR)/docs/icon.svg" 2>/dev/null && \
	mkdir $$tmp/mmt.iconset && \
	for s in 16 32 128 256 512; do \
		sips -z $$s $$s $$tmp/icon.png --out $$tmp/mmt.iconset/icon_$${s}x$${s}.png >/dev/null; \
		sips -z $$((s*2)) $$((s*2)) $$tmp/icon.png --out $$tmp/mmt.iconset/icon_$${s}x$${s}@2x.png >/dev/null; \
	done && \
	iconutil -c icns $$tmp/mmt.iconset -o internal/background/mmt.icns && \
	sips -z 256 256 $$tmp/icon.png --out docs/icon.png >/dev/null && \
	echo "wrote internal/background/mmt.icns and docs/icon.png"
