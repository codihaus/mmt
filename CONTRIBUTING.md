# Contributing

Thanks for helping. Bug reports, fixes and small features are all welcome.

## Setup

```sh
git clone <repo> && cd mmt
make demo   # mock server + mmt, no Mattermost account needed
```

`tools/mockserver` implements only the endpoints mmt calls. When a change
needs a new endpoint, add it there as well so others can try it.

## Before sending a pull request

```sh
make lint
make test
```

CI runs the same checks on macOS and Linux, and builds once with
`CGO_ENABLED=0`.

## Code layout

| Path | Contents |
| --- | --- |
| `main.go` | CLI: `login`, `setup`, `logout`, start-up |
| `internal/mm` | Mattermost API wrapper, user cache, WebSocket loop, sanitizing |
| `internal/ui` | Bubble Tea model: panes, sidebar, input, switcher, rendering |
| `internal/launcher` | Opening mmt in iTerm2 or Ghostty with Cmd key mappings |
| `internal/i18n` | Translations |
| `internal/config` | Config file and keychain |

## Guidelines

- Keep the model single-threaded: only `Update` changes `Model`. Work that
  blocks goes in a `tea.Cmd` and reports back with a message.
- Run anything that comes from the server through the `mm.Clean*` helpers
  before it reaches the screen.
- UI text is written in English and wrapped in `tr("…")`. Add the Vietnamese
  line to `internal/i18n/vi.go`; `go test ./internal/i18n` fails when a string
  has no translation.
- Pass arguments to external commands (`osascript`, `open`, …) as separate
  argv entries, never inside a script or shell string.

## Security issues

Please report them privately to the maintainers instead of opening a public
issue.
