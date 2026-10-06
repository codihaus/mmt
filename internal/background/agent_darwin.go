package background

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

// The agent runs from a small app bundle, because macOS only lets an app
// (something with a bundle identifier) post notifications that can be
// clicked. launchd starts it at login and restarts it if it stops.

const label = "io.github.codihaus.mmt"

// icon is docs/assets/icon.svg as an .icns; `make icon` rebuilds it.
//
//go:embed mmt.icns
var icon []byte

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

func bundlePath() string {
	return filepath.Join(home(), "Library", "Application Support", "mmt", "mmt.app")
}

func bundleExe() string { return filepath.Join(bundlePath(), "Contents", "MacOS", "mmt") }

func plistPath() string {
	return filepath.Join(home(), "Library", "LaunchAgents", label+".plist")
}

// LogPath is where the agent writes what it is doing.
func LogPath() string { return filepath.Join(home(), "Library", "Logs", "mmt-background.log") }

func domain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }

// Supported reports whether this build can run the agent.
func Supported() bool { return nativeNotifications }

// Enabled reports whether the agent is installed.
func Enabled() bool {
	_, err := os.Stat(plistPath())
	return err == nil
}

// Running reports whether launchd has the agent running.
func Running() bool {
	out, err := exec.Command("launchctl", "print", domain()+"/"+label).Output()
	return err == nil && bytes.Contains(out, []byte("state = running"))
}

// Enable installs the agent for exe (the mmt binary that clicks open) and
// starts it. It is safe to run again, e.g. after an upgrade.
func Enable(exe, version string) error {
	if !Supported() {
		return errors.New("this build of mmt has no native notifications")
	}
	if err := writeBundle(exe, version); err != nil {
		return err
	}
	if err := writeFile(plistPath(), agentPlist(exe)); err != nil {
		return err
	}
	stop()
	if out, err := exec.Command("launchctl", "bootstrap", domain(), plistPath()).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// stop unloads the agent and waits until launchd has let go of it; loading
// it again before that fails with an I/O error.
func stop() {
	if exec.Command("launchctl", "bootout", domain()+"/"+label).Run() != nil {
		return
	}
	for i := 0; i < 50 && exec.Command("launchctl", "print", domain()+"/"+label).Run() == nil; i++ {
		time.Sleep(100 * time.Millisecond)
	}
}

// Disable stops the agent and removes it.
func Disable() error {
	stop()
	if err := os.Remove(plistPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_ = exec.Command(lsregister, "-u", bundlePath()).Run()
	return os.RemoveAll(bundlePath())
}

// Restart makes a running agent reload the login (after mmt login/logout).
func Restart() {
	if Enabled() {
		_ = exec.Command("launchctl", "kickstart", "-k", domain()+"/"+label).Run()
	}
}

// Refresh reinstalls the agent when exe differs from the copy it runs, so an
// upgraded mmt also upgrades the agent.
func Refresh(exe, version string) {
	if !Enabled() || !Supported() || sameFile(exe, bundleExe()) {
		return
	}
	_ = Enable(exe, version)
}

func sameFile(a, b string) bool {
	sa, err1 := os.Stat(a)
	sb, err2 := os.Stat(b)
	if err1 != nil || err2 != nil || sa.Size() != sb.Size() {
		return false
	}
	return fileHash(a) == fileHash(b)
}

func fileHash(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	_, _ = io.Copy(h, f)
	return string(h.Sum(nil))
}

func writeBundle(exe, version string) error {
	dst := bundleExe()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	src, err := os.Open(exe)
	if err != nil {
		return err
	}
	defer src.Close()
	// write a new file and rename it over the old one: overwriting a signed
	// binary in place gets it killed by the kernel's signature cache
	tmp := dst + ".new"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, src); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(bundlePath(), "Contents", "Info.plist"), infoPlist(version)); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(bundlePath(), "Contents", "Resources", "mmt.icns"), string(icon)); err != nil {
		return err
	}
	if out, err := exec.Command("codesign", "--force", "--sign", "-", bundlePath()).CombinedOutput(); err != nil {
		return fmt.Errorf("codesign: %s", strings.TrimSpace(string(out)))
	}
	// launchd starts the binary directly, so LaunchServices never sees the
	// app; notifications are refused until it is registered
	if out, err := exec.Command(lsregister, "-f", bundlePath()).CombinedOutput(); err != nil {
		return fmt.Errorf("lsregister: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

const lsregister = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"

func writeFile(p, content string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(content), 0o644)
}

var plists = template.Must(template.New("info").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key><string>{{.Label}}</string>
	<key>CFBundleName</key><string>mmt</string>
	<key>CFBundleDisplayName</key><string>mmt</string>
	<key>CFBundleExecutable</key><string>mmt</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleIconFile</key><string>mmt</string>
	<key>CFBundleShortVersionString</key><string>{{.Version}}</string>
	<key>LSUIElement</key><true/>
	<key>LSMinimumSystemVersion</key><string>11.0</string>
</dict>
</plist>
`))

var agentTmpl = template.Must(template.New("agent").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>{{.Label}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{.Bundle}}</string>
		<string>background</string>
		<string>run</string>
		<string>{{.Exe}}</string>
	</array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>ThrottleInterval</key><integer>30</integer>
	<key>ProcessType</key><string>Interactive</string>
	<key>StandardOutPath</key><string>{{.Log}}</string>
	<key>StandardErrorPath</key><string>{{.Log}}</string>
</dict>
</plist>
`))

func render(t *template.Template, data map[string]string) string {
	for k, v := range data {
		data[k] = xmlEscape(v)
	}
	var b strings.Builder
	_ = t.Execute(&b, data)
	return b.String()
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func infoPlist(version string) string {
	return render(plists, map[string]string{"Label": label, "Version": strings.TrimPrefix(version, "v")})
}

func agentPlist(exe string) string {
	return render(agentTmpl, map[string]string{"Label": label, "Bundle": bundleExe(), "Exe": exe, "Log": LogPath()})
}
