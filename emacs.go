package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type emacs struct {
	server string
	client string
}

func newEmacs() *emacs {
	client, err := exec.LookPath("emacsclient")
	if err != nil {
		log.Printf("emacsclient not found, menu commands are off: %v", err)
	}
	return &emacs{server: "caution-" + strconv.Itoa(os.Getpid()), client: client}
}

func (e *emacs) argv() []string {
	return []string{"emacs", "-nw", "--eval",
		fmt.Sprintf(`(progn (require 'server) (setq server-name %q) (server-start) (xterm-mouse-mode 1) (require 'mwheel))`, e.server)}
}

func (e *emacs) eval(form string) (string, error) {
	if e.client == "" {
		return "", fmt.Errorf("emacsclient not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, e.client, "-s", e.server, "--eval", form).Output()
	return strings.TrimSpace(string(out)), err
}

func (e *emacs) command(candidates ...string) {
	form := fmt.Sprintf(`(run-at-time 0 nil (lambda () (let ((cmd (seq-find #'commandp '(%s)))) (when cmd (call-interactively cmd)))))`,
		strings.Join(candidates, " "))
	go func() {
		if _, err := e.eval(form); err != nil {
			log.Printf("emacs %s: %v", candidates[0], err)
		}
	}()
}

func (e *emacs) selection() (string, error) {
	out, err := e.eval(`(with-current-buffer (window-buffer (selected-window))
  (if (use-region-p)
      (base64-encode-string (encode-coding-string (buffer-substring-no-properties (region-beginning) (region-end)) 'utf-8) t)
    ""))`)
	if err != nil {
		return "", err
	}
	b, err := base64.StdEncoding.DecodeString(strings.Trim(out, `"`))
	return string(b), err
}
