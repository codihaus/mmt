.PHONY: build test lint demo install video video-server

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
	MMT_URL=http://127.0.0.1:8065 MMT_TOKEN=demo ./mmt --here

install:
	go install .

# Screen recording: run `make video-server` in one terminal (off camera, it
# has key controls for mentions, calls and DMs), then `make video` in an
# iTerm2 tab. --here keeps mmt in that tab, so it stays on the mock server.
video-server:
	@mkdir -p dist && go build -o dist/mockserver ./tools/mockserver
	@dist/mockserver -scenario video -addr 127.0.0.1:8065

video: build
	@MMT_URL=http://127.0.0.1:8065 MMT_TOKEN=demo ./mmt --here
