package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestWheelMovesLikeArrowKeys(t *testing.T) {
	bin, err := exec.LookPath("emacs")
	if err != nil {
		t.Skip("emacs not on PATH")
	}
	probe := `(princ (mapconcat (lambda (ev)
  (setq unread-command-events nil)
  (call-interactively (key-binding (vector ev)))
  (format "%s=%S" ev unread-command-events))
  '(wheel-up wheel-down mouse-4 mouse-5) " "))`
	out, err := exec.Command(bin, "-Q", "--batch", "--eval", wheelForm, "--eval", probe).CombinedOutput()
	if err != nil {
		t.Fatalf("emacs: %v\n%s", err, out)
	}
	want := "wheel-up=(up) wheel-down=(down) mouse-4=(up) mouse-5=(down)"
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("wheel events queue\n%s\nwant\n%s", got, want)
	}
}

func TestArgvIncludesWheelForm(t *testing.T) {
	argv := (&emacs{server: "s"}).argv()
	if !strings.Contains(argv[len(argv)-1], wheelForm) {
		t.Fatalf("argv does not install the wheel bindings: %q", argv)
	}
}
