package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	caution "github.com/nullentropy/caution"
	"github.com/nullentropy/caution-term/term"
	"github.com/nullentropy/caution/native"
)

var (
	addr     = flag.String("addr", "", "serve browsers on this address instead of opening a window")
	fontGlob = flag.String("font", term.DefaultFonts, "font files, {a,b} alternatives allowed; empty for the built-in mono")
	fg       = flag.String("fg", "#bbc2cf", "default foreground")
	bg       = flag.String("bg", "#282c34", "default background")
	look     = flag.String("look", "", "start with this look, by name")
	shot     = flag.String("shot", "", "render offscreen to this PNG after -settle ms and exit")
	settle   = flag.Float64("settle", 3000, "ms to wait before -shot")
)

func main() {
	flag.Parse()
	dir := term.PrepareBundle()
	cfg := term.LoadSettings(appName)
	if *look != "" {
		cfg.Look = *look
	}
	save := func() {
		if *shot == "" {
			cfg.Save()
		}
	}
	em := newEmacs()
	argv := flag.Args()
	if len(argv) == 0 {
		argv = em.argv()
	}
	fonts := term.LoadFonts(*fontGlob)

	var upd *updates
	if *addr == "" && *shot == "" {
		if cd, err := os.UserConfigDir(); err == nil {
			upd = startUpdates(filepath.Join(cd, appName))
		}
	}

	mount := func(s *caution.Session) *caution.Node {
		var onExit func()
		if *addr == "" {
			onExit = func() {
				if upd.relaunchRequested() {
					if err := upd.installAndRelaunch(); err != nil {
						log.Printf("update: %v", err)
					}
				}
				os.Exit(0)
			}
		}
		a := newApp(s, em, upd, cfg, save,
			term.WindowOptions{Fonts: fonts, Fg: *fg, Bg: *bg, OnEmpty: onExit},
			term.Options{Argv: argv, Dir: dir})
		return a.root
	}

	caution.ServeClient()
	if *addr != "" {
		log.Fatal(caution.Serve(*addr, mount))
	}
	err := native.Run(mount, native.Options{
		Title: appName, W: cfg.W, H: cfg.H, Maximized: cfg.Maximized,
		CustomTitlebar: true, TitlebarStyle: "compact",
		OnWindow: func(w, h int, maximized bool) {
			cfg.W, cfg.H, cfg.Maximized = w, h, maximized
			save()
		},
		Shot: *shot, SettleMs: *settle,
	})
	if err != nil {
		log.Fatal(err)
	}
}

func isEmacs(argv []string) bool {
	return len(argv) > 0 && filepath.Base(argv[0]) == "emacs"
}
