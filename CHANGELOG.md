# Changelog

All notable changes to mmt are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
Beta builds are tagged `v0.1.0-beta.YYYYMMDD`, with a `.N` suffix for a second
build on the same day.

## [Unreleased]

## [0.1.0-beta.20261013] - 2026-10-13

### Added

- A new-messages bar in the line under the channel name: every other
  conversation with unread messages, newest first, with a short name and the
  mention count. A message to a channel scrolled out of the sidebar is no
  longer missed. Click a name to open it. Works alongside the Unreads filter.
- Delete your own messages: select one with `↑`, press `d`, then `Enter` to
  confirm. The message disappears at once and comes back if the server
  refuses. Enter confirms rather than a second `d`, because Vietnamese Telex
  turns `dd` into `đ`.
- This changelog. The README links it and the landing page.

### Changed

- `/call` starts a call when none is running and joins it, instead of only
  opening the channel in the browser.
- The lock screen waits for the passcode; `Esc` brings up Touch ID. The Touch
  ID dialog no longer pops up on its own when mmt locks, idle locks included.

### Fixed

- Clicking a notification from an open mmt window in Terminal.app (or any
  terminal without OSC 9 notifications) opened Script Editor. The background
  agent now shows these notifications and a click brings the terminal back;
  without the agent, mmt rings the bell instead.
- Deleting a thread's root message also removes its replies, which used to
  stay in the channel.

## [0.1.0-beta.20261006] - 2026-10-06

### Added

- Reactions: select a message with `↑` and press `e`, pick one of eight with
  a number key or type to search every emoji on the server. `+:thumbsup:`
  reacts to the latest message.
- Bot cards: message attachments from integrations show their colored bar,
  title, fields, image and footer. Buttons open the message on the web.
- `/upload`, a file browser: newest files first, a preview on the right, type
  to search the folder and everything below it, `Tab` to pick several.
- `/lock` locks mmt right away, like `Ctrl+L`.
- `mmt --new` opens another window for the same server.
- A landing page with a short recording of each feature:
  https://codihaus.github.io/mmt/

### Changed

- One window per server: opening mmt again closes the window already running
  for that server, so messages are no longer announced twice.
- `@` suggestions list only channel members, so nobody is mentioned in a
  channel they cannot read.

### Fixed

- `mmt --help` in Vietnamese showed the English text.

## [0.1.0-beta.20261001.3] - 2026-10-01

### Added

- Unreads filter: a button at the top of the sidebar (or `Alt+U`,
  `Cmd+Shift+U` in iTerm2/Ghostty, or `/unreads`) shows only conversations
  with something new. mmt remembers the choice.

### Changed

- Calls open in the browser instead of the desktop app, from a call card, a
  call notification or `/call`.
- `install.sh` checks the binary is there before replacing the old one.

### Fixed

- mmt no longer overwrites settings (language, browser) changed while it was
  open.

## [0.1.0-beta.20261001.2] - 2026-10-01

### Added

- Windows build (preview): a zip with an install script, Win32 clipboard and
  toast notifications.
- Install scripts in the repo, and `make release` to build every archive.

### Fixed

- Upgrading on macOS: `install.sh` removes the old binary first, because
  overwriting a signed binary in place gets it killed.

## [0.1.0-beta.20261001] - 2026-10-01

First public beta.

### Added

- The web sidebar as-is: categories, order, collapsed sections, muted
  channels, unread and mention badges.
- Threads in a side panel, markdown, reactions, inline images on iTerm2.
- `Ctrl+K` switcher that ignores accents and finds channels you have not
  joined.
- Paste screenshots, drop files to attach, mouse support, text selection.
- Admin commands with small forms: channels, members, bots, tokens.
- `!command` runs a shell command locally and can post the output.
- `mmt background on`: notifications for mentions, DMs and calls even when
  mmt is closed (macOS).
- `mmt lock`: Touch ID or passcode lock, separate from the Mattermost login.
- English and Vietnamese UI.
- `make demo` runs mmt against a mock server, no Mattermost needed.

[Unreleased]: https://github.com/codihaus/mmt/compare/v0.1.0-beta.20261013...HEAD
[0.1.0-beta.20261013]: https://github.com/codihaus/mmt/compare/v0.1.0-beta.20261006...v0.1.0-beta.20261013
[0.1.0-beta.20261006]: https://github.com/codihaus/mmt/compare/v0.1.0-beta.20261001.3...v0.1.0-beta.20261006
[0.1.0-beta.20261001.3]: https://github.com/codihaus/mmt/compare/v0.1.0-beta.20261001.2...v0.1.0-beta.20261001.3
[0.1.0-beta.20261001.2]: https://github.com/codihaus/mmt/compare/v0.1.0-beta.20261001...v0.1.0-beta.20261001.2
[0.1.0-beta.20261001]: https://github.com/codihaus/mmt/releases/tag/v0.1.0-beta.20261001
