package main

import (
	"context"
	"log"
	"strings"
	"time"

	caution "github.com/nullentropy/caution"
	"github.com/nullentropy/caution-term/term"
)

type app struct {
	s    *caution.Session
	cfg  *term.Settings
	save func()
	em   *emacs
	w    *term.Window
	t    *term.Term
	upd  *updates
	root *caution.Node
	dlg  *caution.Node
}

func newApp(s *caution.Session, em *emacs, upd *updates, cfg *term.Settings, save func(), wo term.WindowOptions, o term.Options) *app {
	a := &app{s: s, cfg: cfg, save: save, em: em, upd: upd}
	wo.FontSize, wo.Look = cfg.FontSize, cfg.Look
	a.w = term.NewWindow(s, wo)
	a.t = a.w.Open(o)
	fill := caution.A{Left: caution.Px(0), Right: caution.Px(0), Top: caution.Px(0), Bottom: caution.Px(0)}
	a.root = caution.Panel().Kids(a.w.Node().Anchor(fill))
	if upd != nil {
		upd.setOnReady(func() { s.Update(a.buildMenu) })
	}
	a.buildMenu()
	return a
}

func (a *app) copySelection() {
	if a.t.CopySelection() {
		return
	}
	go func() {
		text, err := a.em.selection()
		if err != nil {
			log.Printf("copy: %v", err)
			return
		}
		if text != "" {
			a.s.Update(func() { a.s.WriteClipboard(text) })
		}
	}()
}

func (a *app) buildMenu() {
	em := a.em
	cmd := func(title, key string, candidates ...string) caution.MenuItem {
		return caution.MenuItem{Title: title, Key: key, OnPick: func() { em.command(candidates...) }}
	}
	sep := caution.MenuItem{Sep: true}

	view := term.ViewMenu(a.w, func() {
		a.cfg.FontSize, a.cfg.Look = a.w.FontSize(), a.w.Look()
		a.save()
		a.buildMenu()
	})
	appItems := []caution.MenuItem{
		{Title: "Check for Updates…", OnPick: a.checkForUpdates},
	}
	if tag := a.upd.readyTag(); tag != "" && a.canRestart() {
		appItems = append(appItems, caution.MenuItem{Title: "Restart to Update to " + strings.TrimPrefix(tag, "v"), OnPick: a.restartToUpdate})
	}
	appMenu := caution.Menu{App: true, Items: appItems}
	if em.client == "" || !isEmacs(a.t.Argv()) {
		a.s.SetMenu(appMenu, view)
		return
	}
	appMenu.Items = append(appMenu.Items, sep,
		cmd("Open Doom Config", "cmd+,", "doom/open-private-config"),
		cmd("Reload Doom Config", "cmd+shift+,", "doom/reload"))
	a.s.SetMenu([]caution.Menu{
		appMenu,
		{Title: "File", Items: []caution.MenuItem{
			cmd("Open File…", "cmd+o", "find-file"),
			cmd("Open Recent…", "cmd+shift+o", "consult-recent-file", "recentf-open-files"),
			cmd("Open File in Project…", "cmd+p", "projectile-find-file", "project-find-file"),
			cmd("Switch Project…", "cmd+shift+p", "projectile-switch-project", "project-switch-project"),
			sep,
			cmd("Save", "cmd+s", "save-buffer"),
			cmd("Save As…", "cmd+shift+s", "write-file"),
			sep,
			cmd("Close Buffer", "cmd+w", "kill-current-buffer"),
		}},
		{Title: "Edit", Items: []caution.MenuItem{
			cmd("Undo", "cmd+z", "undo-fu-only-undo", "undo"),
			cmd("Redo", "cmd+shift+z", "undo-fu-only-redo", "undo-redo"),
			sep,
			{Title: "Copy", Key: "cmd+c", OnPick: a.copySelection},
			{Title: "Paste", Key: "cmd+v", OnPick: a.t.PasteClipboard},
			cmd("Select All", "cmd+a", "mark-whole-buffer"),
			sep,
			cmd("Find in Buffer…", "cmd+f", "+default/search-buffer", "consult-line", "isearch-forward"),
			cmd("Find in Project…", "cmd+shift+f", "+default/search-project", "consult-ripgrep", "project-find-regexp"),
		}},
		view,
		{Title: "Emacs", Items: []caution.MenuItem{
			cmd("Run Command…", "cmd+shift+x", "execute-extended-command"),
			cmd("Switch Buffer…", "cmd+b", "consult-buffer", "switch-to-buffer"),
			cmd("Eval Expression…", "cmd+shift+e", "eval-expression"),
			sep,
			cmd("Split Right", "cmd+d", "split-window-right"),
			cmd("Split Below", "cmd+shift+d", "split-window-below"),
			cmd("Close Other Windows", "cmd+shift+1", "delete-other-windows"),
			sep,
			cmd("Magit Status", "cmd+g", "magit-status"),
			cmd("Describe Key…", "cmd+shift+k", "describe-key"),
			sep,
			{Title: "Restart Emacs", Key: "cmd+shift+r", OnPick: a.t.Restart},
		}},
	}...)
}

func shortVersion() string { return strings.TrimPrefix(version, "v") }

// canRestart reports whether emacs can be asked to save and quit
func (a *app) canRestart() bool {
	return a.em.client != "" && isEmacs(a.t.Argv())
}

func (a *app) checkForUpdates() {
	if a.upd == nil {
		if _, ok := parseTag(version); !ok {
			a.dialog("Updates are off", "This is a development build of "+appName+", which does not update itself.")
		} else {
			a.dialog("Updates are off", "This copy of "+appName+" does not update itself. Install it with install.sh to get updates.")
		}
		return
	}
	a.dialog("Checking for updates…", "")
	wait := a.dlg
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		tag, err := a.upd.check(ctx)
		cancel()
		a.s.Update(func() {
			if a.dlg != wait {
				return
			}
			have := "You have version " + shortVersion() + "."
			next := appName + " " + strings.TrimPrefix(tag, "v") + " is ready"
			switch {
			case err != nil:
				a.dialog("Could not check for updates", sentence(err))
			case tag == "":
				a.dialog(appName+" is up to date", have)
			case a.canRestart():
				a.dialog(next, have+" Restart to update. Emacs will ask to save your buffers first.",
					caution.Button("Later").OnClick(a.closeDialog),
					caution.Button("Restart").Primary().OnClick(a.restartToUpdate))
			default:
				a.dialog(next, have+" The update installs the next time you open "+appName+".")
			}
		})
	}()
}

func (a *app) restartToUpdate() {
	a.closeDialog()
	if a.upd == nil || !a.canRestart() {
		return
	}
	go func() {
		if _, err := a.em.eval(restartForm(a.upd.marker)); err != nil {
			a.s.Update(func() {
				a.dialog("Could not update", sentence(err)+" "+appName+" is still running the current version.")
			})
		}
	}()
}

func sentence(err error) string {
	s := err.Error()
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:] + "."
}

func (a *app) dialog(title, body string, buttons ...*caution.Node) {
	a.closeDialog()
	if len(buttons) == 0 && body != "" {
		buttons = []*caution.Node{caution.Button("OK").Primary().OnClick(a.closeDialog)}
	}
	h := 110.0
	if body != "" {
		h = 170
	}
	d := caution.Dialog(title).CardSize(460, h)
	if body != "" {
		d.Add(caution.Label(body).FontSize(13).Color("$inkDim").Wrap().
			Anchor(caution.A{Left: caution.Px(20), Right: caution.Px(20), Top: caution.Px(16)}))
	}
	if len(buttons) > 0 {
		d.Add(caution.HStack().Gap(8).Anchor(caution.A{Right: caution.Px(20), Bottom: caution.Px(20)}).Kids(buttons...))
	}
	d.OnDismiss(a.closeDialog)
	a.dlg = d
	a.root.Add(d)
}

func (a *app) closeDialog() {
	if a.dlg == nil {
		return
	}
	a.dlg.Remove()
	a.dlg = nil
	a.w.Step(0)
}
