package main

import (
	"log"

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
}

func newApp(s *caution.Session, em *emacs, cfg *term.Settings, save func(), wo term.WindowOptions, o term.Options) *app {
	a := &app{s: s, cfg: cfg, save: save, em: em}
	wo.FontSize, wo.Look = cfg.FontSize, cfg.Look
	a.w = term.NewWindow(s, wo)
	a.t = a.w.Open(o)
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
	if em.client == "" || !isEmacs(a.t.Argv()) {
		a.s.SetMenu(view)
		return
	}
	a.s.SetMenu([]caution.Menu{
		{App: true, Items: []caution.MenuItem{
			cmd("Open Doom Config", "cmd+,", "doom/open-private-config"),
			cmd("Reload Doom Config", "cmd+shift+,", "doom/reload"),
		}},
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
