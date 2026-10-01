// mmt is a terminal client for Mattermost.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattermost/mattermost/server/public/model"
	"golang.org/x/term"

	"github.com/codihaus/mmt/internal/background"
	"github.com/codihaus/mmt/internal/config"
	"github.com/codihaus/mmt/internal/i18n"
	"github.com/codihaus/mmt/internal/launcher"
	"github.com/codihaus/mmt/internal/lock"
	"github.com/codihaus/mmt/internal/mm"
	"github.com/codihaus/mmt/internal/ui"
)

// usage is English source text; i18n translates it.
const usage = `mmt — Mattermost in the terminal

  mmt          start (opens in iTerm2/Ghostty when configured)
  mmt --here   run in the current terminal
  mmt login    log in (the token is kept in the system keychain)
  mmt setup    choose the terminal and the interface language
  mmt lock     set up the app lock (Touch ID or a passcode)
  mmt background on|off|status
               notifications while mmt is closed (macOS)
  mmt logout   remove the stored token

Environment: MMT_URL, MMT_TOKEN (override config and keychain), MMT_LANG (en, vi).
`

// version is set at build time: -ldflags "-X main.version=v0.1.0".
var version = "dev"

func init() {
	// Cocoa, used by the background agent, must own the main thread
	runtime.LockOSThread()
}

func main() {
	if cfg, err := config.Load(); err == nil {
		i18n.Set(i18n.Detect(cfg.Language))
	}
	var err error
	switch arg := firstArg(); arg {
	case "":
		err = run(false)
	case "--here":
		err = run(true)
	case "login":
		err = login()
	case "setup":
		err = setup()
	case "lock":
		err = setupLock()
	case "background":
		err = backgroundCmd()
	case "logout":
		err = logout()
	case "-v", "--version", "version":
		fmt.Println("mmt", version)
	case "-h", "--help", "help":
		fmt.Print(i18n.T(usage))
	default:
		err = fmt.Errorf(i18n.T("unknown command %q\n\n%s"), arg, i18n.T(usage))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "mmt:", err)
		os.Exit(1)
	}
}

func firstArg() string {
	if len(os.Args) > 1 {
		return os.Args[1]
	}
	return ""
}

func run(here bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.ServerURL == "" {
		return errors.New(i18n.T("not logged in, run `mmt login` first"))
	}
	if target := launcher.Resolve(launcher.Kind(cfg.Terminal)); !here && launcher.ShouldRelaunch(target) {
		exe, err := os.Executable()
		if err == nil {
			err = launcher.Launch(target, exe)
		}
		if err == nil {
			fmt.Printf(i18n.T("Opened mmt in %s. (Run `mmt --here` to use it right here.)\n"), target.Label())
			return nil
		}
		fmt.Fprintf(os.Stderr, i18n.T("Could not open %s (%v); running here.\n"), target.Label(), err)
	}
	// the app lock comes before any network or keychain access to the token
	if err := unlock(cfg); err != nil {
		return err
	}
	token, err := config.Token(cfg.ServerURL)
	if err != nil || token == "" {
		return errors.New(i18n.T("no token found, run `mmt login`"))
	}

	c := mm.New(cfg.ServerURL, token)
	cx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	err = c.Connect(cx)
	cancel()
	if err != nil {
		return fmt.Errorf(i18n.T("cannot connect to %s: %w (token expired? run `mmt login`)"), cfg.ServerURL, err)
	}

	// inside iTerm2, switch this tab to the mmt profile for Cmd shortcuts
	if restore, ok := launcher.AdoptITermProfile(); ok {
		ui.EnableCmdKeys()
		defer restore()
	}

	defer background.HoldUI()()
	if exe, err := os.Executable(); err == nil {
		go background.Refresh(exe, version)
	}

	app := ui.New(c, cfg)
	defer app.Cleanup()
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())

	wsCtx, stop := context.WithCancel(context.Background())
	defer stop()
	go c.Listen(wsCtx,
		func(ev *model.WebSocketEvent) { p.Send(ui.WSEventMsg{Ev: ev}) },
		func(ok bool, err error) { p.Send(ui.WSStateMsg{Connected: ok, Err: err}) },
	)

	_, err = p.Run()
	return err
}

func login() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	in := bufio.NewReader(os.Stdin)
	ask := func(prompt, def string) string {
		if def != "" {
			fmt.Printf("%s [%s]: ", prompt, def)
		} else {
			fmt.Printf("%s: ", prompt)
		}
		s, _ := in.ReadString('\n')
		if s = strings.TrimSpace(s); s == "" {
			return def
		}
		return s
	}
	secret := func(prompt string) string {
		fmt.Printf("%s: ", prompt)
		b, _ := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		return strings.TrimSpace(string(b))
	}

	server := config.NormalizeURL(ask("Server", cfg.ServerURL))
	if server == "" {
		return errors.New(i18n.T("a server URL is required"))
	}
	if strings.HasPrefix(server, "http://") && !strings.Contains(server, "://localhost") && !strings.Contains(server, "://127.0.0.1") {
		fmt.Fprintln(os.Stderr, i18n.T("Warning: this server uses plain http, so your token is sent unencrypted."))
	}
	fmt.Println(i18n.T("Paste a Personal Access Token (recommended), or press Enter to log in with username and password."))
	token := secret("Token")

	cx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if token == "" {
		user := ask("Username/email", "")
		pass := secret("Password")
		token, err = mm.Login(cx, server, user, pass, "")
		var appErr *model.AppError
		if errors.As(err, &appErr) && strings.Contains(appErr.Id, "mfa") {
			token, err = mm.Login(cx, server, user, pass, ask(i18n.T("MFA code"), ""))
		}
		if err != nil {
			return fmt.Errorf(i18n.T("login failed: %w"), err)
		}
	}

	c := mm.New(server, token)
	if err := c.Connect(cx); err != nil {
		return fmt.Errorf(i18n.T("invalid token: %w"), err)
	}
	if err := config.SetToken(server, token); err != nil {
		return fmt.Errorf(i18n.T("cannot save to the keychain: %w"), err)
	}
	cfg.ServerURL = server
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf(i18n.T("Logged in to %s as @%s.\n\n"), server, c.Me.Username)
	if err := chooseLanguage(cfg, in); err != nil {
		return err
	}
	if err := chooseTerminal(cfg, in); err != nil {
		return err
	}
	if err := chooseBackground(in); err != nil {
		return err
	}
	background.Restart()
	fmt.Println(i18n.T("Run `mmt` to start."))
	return nil
}

func setup() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	in := bufio.NewReader(os.Stdin)
	if err := chooseLanguage(cfg, in); err != nil {
		return err
	}
	if err := chooseTerminal(cfg, in); err != nil {
		return err
	}
	return chooseBackground(in)
}

// chooseBackground asks whether to keep notifications coming after mmt is
// closed, and installs or removes the background agent.
func chooseBackground(in *bufio.Reader) error {
	if !background.Supported() {
		return nil
	}
	on := background.Enabled()
	prompt := i18n.T("Show notifications for mentions and direct messages while mmt is closed? [Y/n]: ")
	if on {
		prompt = i18n.T("Notifications while mmt is closed are on. Keep them? [Y/n]: ")
	}
	fmt.Print(prompt)
	s, _ := in.ReadString('\n')
	s = strings.ToLower(strings.TrimSpace(s))
	want := s != "n" && s != "no" && s != "k" && s != "không"
	if want == on {
		return nil
	}
	return setBackground(want)
}

func setBackground(on bool) error {
	if !on {
		if err := background.Disable(); err != nil {
			return err
		}
		fmt.Println(i18n.T("Background notifications are off."))
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := background.Enable(exe, version); err != nil {
		return err
	}
	fmt.Println(i18n.T("Background notifications are on. macOS may ask once whether mmt can send notifications; allow it."))
	return nil
}

// backgroundCmd is `mmt background on|off|status`; `run` is what launchd
// starts.
func backgroundCmd() error {
	sub := ""
	if len(os.Args) > 2 {
		sub = os.Args[2]
	}
	switch sub {
	case "on":
		return setBackground(true)
	case "off":
		return setBackground(false)
	case "run":
		exe := ""
		if len(os.Args) > 3 {
			exe = os.Args[3]
		}
		return background.Run(exe)
	case "", "status":
		switch {
		case !background.Supported():
			fmt.Println(i18n.T("Background notifications need the macOS build of mmt."))
		case !background.Enabled():
			fmt.Println(i18n.T("Background notifications are off. Turn them on with `mmt background on`."))
		case background.Blocked():
			fmt.Println(i18n.T("Background notifications are on, but macOS is blocking them. Allow mmt in System Settings → Notifications."))
		case background.Running():
			fmt.Println(i18n.T("Background notifications are on and running."))
		default:
			fmt.Println(i18n.T("Background notifications are on but not running; see"), background.LogPath())
		}
		return nil
	}
	return fmt.Errorf(i18n.T("unknown command %q\n\n%s"), "background "+sub, i18n.T(usage))
}

// chooseLanguage asks for the interface language and saves it.
func chooseLanguage(cfg *config.Config, in *bufio.Reader) error {
	names := map[string]string{"en": "English", "vi": "Tiếng Việt"}
	langs := i18n.Languages()
	def := 1
	fmt.Println(i18n.T("Interface language") + ":")
	for i, l := range langs {
		mark := " "
		if l == i18n.Lang() {
			mark, def = "*", i+1
		}
		fmt.Printf(" %s %d. %s\n", mark, i+1, names[l])
	}
	fmt.Printf(i18n.T("Choose [%d]: "), def)
	line, _ := in.ReadString('\n')
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(langs) {
		n = def
	}
	cfg.Language = langs[n-1]
	i18n.Set(cfg.Language)
	fmt.Println()
	return cfg.Save()
}

// chooseTerminal asks which terminal mmt should open in and saves it.
func chooseTerminal(cfg *config.Config, in *bufio.Reader) error {
	kinds := []launcher.Kind{launcher.Auto, launcher.ITerm, launcher.Ghostty, launcher.Current}
	cur := launcher.Kind(cfg.Terminal)
	if cur == "" {
		cur = launcher.Auto
	}
	fmt.Println(i18n.T("Choose the terminal mmt runs in (Terminal.app cannot pass Cmd shortcuts through):"))
	def := 1
	for i, k := range kinds {
		note := ""
		if (k == launcher.ITerm || k == launcher.Ghostty) && !launcher.Installed(k) {
			note = i18n.T("  (not installed)")
		}
		if k == launcher.Auto {
			note = i18n.T("  → currently: ") + launcher.Resolve(launcher.Auto).Label()
		}
		mark := " "
		if k == cur {
			mark, def = "*", i+1
		}
		fmt.Printf(" %s %d. %s%s\n", mark, i+1, k.Label(), note)
	}
	fmt.Printf(i18n.T("Choose [%d]: "), def)
	line, _ := in.ReadString('\n')
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(kinds) {
		n = def
	}
	pick := kinds[n-1]

	if (pick == launcher.ITerm || pick == launcher.Ghostty) && !launcher.Installed(pick) {
		cask := map[launcher.Kind]string{launcher.ITerm: "iterm2", launcher.Ghostty: "ghostty"}[pick]
		fmt.Printf(i18n.T("%s is not installed. Install it with `brew install --cask %s`? [y/N]: "), pick.Label(), cask)
		ans, _ := in.ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(ans)); a == "y" || a == "yes" {
			cmd := exec.Command("brew", "install", "--cask", cask)
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Errorf(i18n.T("installing %s failed: %w"), pick.Label(), err)
			}
		} else {
			fmt.Println(i18n.T("Skipped; mmt runs in the current terminal until the app is installed."))
		}
	}
	cfg.Terminal = string(pick)
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf(i18n.T("Saved: %s. Change it any time with `mmt setup`.\n\n"), pick.Label())
	return nil
}

func logout() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.ServerURL == "" {
		return nil
	}
	// end the server session too (password logins); personal access tokens
	// stay valid until revoked in the web app
	if token, err := config.Token(cfg.ServerURL); err == nil && token != "" {
		cx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = mm.New(cfg.ServerURL, token).Logout(cx)
		cancel()
	}
	if err := config.DeleteToken(cfg.ServerURL); err != nil {
		return err
	}
	background.Restart()
	fmt.Println(i18n.T("Removed the token for"), cfg.ServerURL)
	return nil
}

// unlock asks for Touch ID or the mmt passcode before anything is shown.
func unlock(cfg *config.Config) error {
	if cfg.Lock == lock.ModeOff {
		return nil
	}
	if cfg.Lock == lock.ModeTouchID && lock.TouchIDAvailable() {
		if ok, _ := lock.TouchID(i18n.T("unlock mmt")); ok {
			return nil
		}
	}
	if !lock.HasPasscode() {
		return errors.New(i18n.T("the app lock has no passcode; run `mmt lock`"))
	}
	for try := 0; try < 3; try++ {
		fmt.Print(i18n.T("mmt passcode: "))
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return err
		}
		if ok, err := lock.Verify(string(b)); err != nil {
			return err
		} else if ok {
			return nil
		}
		fmt.Fprintln(os.Stderr, i18n.T("Wrong passcode."))
	}
	return errors.New(i18n.T("too many wrong passcodes"))
}

// setupLock turns the app lock on or off and sets the passcode. Changing an
// active lock needs the current unlock first.
func setupLock() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Lock != lock.ModeOff {
		if err := unlock(cfg); err != nil {
			return err
		}
	}
	in := bufio.NewReader(os.Stdin)
	touch := lock.TouchIDAvailable()
	fmt.Println(i18n.T("App lock for mmt (separate from your Mattermost login):"))
	opts := []string{lock.ModeTouchID, lock.ModePasscode, lock.ModeOff}
	labels := map[string]string{
		lock.ModeTouchID:  i18n.T("Touch ID, with a passcode as fallback"),
		lock.ModePasscode: i18n.T("Passcode only"),
		lock.ModeOff:      i18n.T("Off"),
	}
	def := 3
	for i, o := range opts {
		mark, note := " ", ""
		if o == cfg.Lock {
			mark, def = "*", i+1
		}
		if o == lock.ModeTouchID && !touch {
			note = i18n.T("  (Touch ID not available here)")
		}
		fmt.Printf(" %s %d. %s%s\n", mark, i+1, labels[o], note)
	}
	fmt.Printf(i18n.T("Choose [%d]: "), def)
	line, _ := in.ReadString('\n')
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(opts) {
		n = def
	}
	mode := opts[n-1]
	if mode == lock.ModeOff {
		cfg.Lock = lock.ModeOff
		if err := lock.ClearPasscode(); err != nil {
			return err
		}
		fmt.Println(i18n.T("App lock is off."))
		return cfg.Save()
	}
	if !lock.HasPasscode() || askYes(in, i18n.T("Change the passcode? [y/N]: ")) {
		for {
			fmt.Print(i18n.T("New passcode: "))
			a, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Println()
			if err != nil {
				return err
			}
			fmt.Print(i18n.T("Repeat it: "))
			b, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Println()
			if err != nil {
				return err
			}
			if string(a) != string(b) {
				fmt.Fprintln(os.Stderr, i18n.T("The passcodes do not match."))
				continue
			}
			if err := lock.SetPasscode(string(a)); err != nil {
				fmt.Fprintln(os.Stderr, err)
				continue
			}
			break
		}
	}
	after := cfg.LockAfter
	if after == 0 {
		after = 10
	}
	fmt.Printf(i18n.T("Lock after how many idle minutes? 0 = never [%d]: "), after)
	line, _ = in.ReadString('\n')
	if v, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && v >= 0 {
		after = v
	}
	cfg.Lock, cfg.LockAfter = mode, after
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Println(i18n.T("App lock is on. Ctrl+L locks right away."))
	return nil
}

func askYes(in *bufio.Reader, prompt string) bool {
	fmt.Print(prompt)
	s, _ := in.ReadString('\n')
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "y" || s == "yes" || s == "c" || s == "có"
}
