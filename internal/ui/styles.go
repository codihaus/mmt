package ui

import (
	"hash/fnv"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/codihaus/mmt/internal/i18n"
	"github.com/codihaus/mmt/internal/launcher"
)

var (
	colAccent  = lipgloss.AdaptiveColor{Light: "#1c58d9", Dark: "#5d8ef5"}
	colDim     = lipgloss.AdaptiveColor{Light: "#8a8a8a", Dark: "#6c6c6c"}
	colText    = lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#e4e4e4"}
	colBorder  = lipgloss.AdaptiveColor{Light: "#d0d0d0", Dark: "#3a3a3a"}
	colSelBg   = lipgloss.AdaptiveColor{Light: "#e3ebfb", Dark: "#23324f"}
	colErr     = lipgloss.AdaptiveColor{Light: "#c62828", Dark: "#ef5350"}
	colOK      = lipgloss.AdaptiveColor{Light: "#2e7d32", Dark: "#66bb6a"}
	colBadge   = lipgloss.AdaptiveColor{Light: "#d32f2f", Dark: "#e53935"}
	colMention = lipgloss.AdaptiveColor{Light: "#8a5a00", Dark: "#ffca28"}

	stDim     = lipgloss.NewStyle().Foreground(colDim)
	stErr     = lipgloss.NewStyle().Foreground(colErr)
	stOK      = lipgloss.NewStyle().Foreground(colOK)
	stAccent  = lipgloss.NewStyle().Foreground(colAccent)
	stMention = lipgloss.NewStyle().Bold(true).Foreground(colMention)
	stCode    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#6a1b9a", Dark: "#ce93d8"})
	stBadge   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Background(colBadge)
	stTitle   = lipgloss.NewStyle().Bold(true).Foreground(colText)
	stSection = lipgloss.NewStyle().Bold(true).Foreground(colDim)
	stGutter  = lipgloss.NewStyle().Foreground(colAccent)
	stLink    = lipgloss.NewStyle().Foreground(colAccent).Underline(true)

	stSideCur    = lipgloss.NewStyle().Foreground(colText).Background(colSelBg).Bold(true)
	stSideCursor = lipgloss.NewStyle().Reverse(true)
	stSideUnread = lipgloss.NewStyle().Foreground(colText).Bold(true)
	stSideNormal = lipgloss.NewStyle().Foreground(colDim)
	stSideMuted  = lipgloss.NewStyle().Foreground(colBorder)

	stInputBox   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colBorder).Padding(0, 1)
	stInputOn    = stInputBox.BorderForeground(colAccent)
	stInputShell = stInputBox.BorderForeground(colMention)
	stPopup      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colAccent).Padding(0, 1)
	stSugSel     = lipgloss.NewStyle().Foreground(colText).Background(colSelBg).Bold(true)
)

var userPalette = []lipgloss.AdaptiveColor{
	{Light: "#c62828", Dark: "#ef9a9a"},
	{Light: "#2e7d32", Dark: "#a5d6a7"},
	{Light: "#1565c0", Dark: "#90caf9"},
	{Light: "#6a1b9a", Dark: "#ce93d8"},
	{Light: "#ef6c00", Dark: "#ffcc80"},
	{Light: "#00838f", Dark: "#80deea"},
	{Light: "#ad1457", Dark: "#f48fb1"},
	{Light: "#4e342e", Dark: "#bcaaa4"},
}

func userStyle(id string) lipgloss.Style {
	h := fnv.New32a()
	h.Write([]byte(id))
	return lipgloss.NewStyle().Bold(true).Foreground(userPalette[h.Sum32()%uint32(len(userPalette))])
}

// cmdKeys is set when mmt was opened by the launcher in a terminal that maps
// Cmd shortcuts, so on-screen hints can show the Mac keys.
var cmdKeys = os.Getenv(launcher.CmdKeysEnv) == "1"

// EnableCmdKeys switches hints to Cmd keys once the terminal maps them.
func EnableCmdKeys() { cmdKeys = true }

var macKeys = strings.NewReplacer(
	"Ctrl+K", "Cmd+K", "Ctrl+V", "Cmd+V", "Ctrl+T", "Cmd+T",
	"Alt+↑↓", "Cmd+↑↓", "Alt+↑ / Alt+↓", "Cmd+↑ / Cmd+↓", "Alt+A", "Cmd+Shift+A", "Alt+U", "Cmd+Shift+U",
)

// tr translates an English UI string into the configured language.
func tr(s string) string { return i18n.T(s) }

// keys rewrites shortcut hints for the current terminal.
func keys(s string) string {
	if cmdKeys {
		return macKeys.Replace(s)
	}
	return s
}
