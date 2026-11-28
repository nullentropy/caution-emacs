package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNewerTag(t *testing.T) {
	cases := []struct {
		cur, next string
		want      bool
	}{
		{"0.3.1", "v0.3.2", true},
		{"0.3.1", "v0.4.0", true},
		{"0.3.1", "v0.3.1", false},
		{"0.3.1", "v0.3.0", false},
		{"0.10.0", "v0.9.9", false},
		{"dev", "v9.9.9", false},
		{"0.3.1", "nightly", false},
	}
	for _, c := range cases {
		if got := newerTag(c.cur, c.next); got != c.want {
			t.Errorf("newerTag(%q, %q) = %v, want %v", c.cur, c.next, got, c.want)
		}
	}
}

func appZip(t *testing.T, marker string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(appName + ".app/Contents/MacOS/caution-emacs")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte(marker))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type fakeRelease struct {
	tag  string
	zip  []byte
	sums string
	hits int
}

func (f *fakeRelease) serve(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/" + updateRepo + "/releases/latest":
			fmt.Fprintf(w, `{"tag_name":%q}`, f.tag)
		case "/" + updateRepo + "/releases/download/" + f.tag + "/" + updateAsset:
			f.hits++
			w.Write(f.zip)
		case "/" + updateRepo + "/releases/download/" + f.tag + "/SHA256SUMS":
			fmt.Fprint(w, f.sums)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newFake(t *testing.T, tag, marker string) *fakeRelease {
	z := appZip(t, marker)
	sum := sha256.Sum256(z)
	return &fakeRelease{tag: tag, zip: z,
		sums: hex.EncodeToString(sum[:]) + "  " + updateAsset + "\nabc  CautionEmacs-x.zip\n"}
}

func testUpdater(t *testing.T, ver string, srv *httptest.Server) updater {
	u := newUpdater(t.TempDir())
	u.version, u.api, u.dl = ver, srv.URL, srv.URL
	return u
}

func TestCheckAndStageStagesNewerRelease(t *testing.T) {
	f := newFake(t, "v0.4.0", "new")
	u := testUpdater(t, "0.3.1", f.serve(t))
	tag, err := u.checkAndStage(context.Background())
	if err != nil || tag != "v0.4.0" {
		t.Fatalf("checkAndStage = %q, %v", tag, err)
	}
	if got := u.readyTag(); got != "v0.4.0" {
		t.Fatalf("readyTag = %q", got)
	}
	s, err := u.readStaged()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(s.Payload, "Contents/MacOS/caution-emacs"))
	if err != nil || string(b) != "new" {
		t.Fatalf("staged binary = %q, %v", b, err)
	}

	if _, err := u.checkAndStage(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.hits != 1 {
		t.Fatalf("downloaded %d times, want 1 when already staged", f.hits)
	}
}

func TestCheckAndStageRejectsBadChecksum(t *testing.T) {
	f := newFake(t, "v0.4.0", "new")
	f.sums = "0000  " + updateAsset + "\n"
	u := testUpdater(t, "0.3.1", f.serve(t))
	if _, err := u.checkAndStage(context.Background()); err == nil {
		t.Fatal("staged a release whose checksum does not match")
	}
	if u.readyTag() != "" {
		t.Fatal("a failed download left an update staged")
	}
	if _, err := os.Stat(u.dir); !os.IsNotExist(err) {
		t.Fatalf("update dir left behind: %v", err)
	}
}

func TestCheckAndStageSkipsSameOrDev(t *testing.T) {
	f := newFake(t, "v0.3.1", "same")
	srv := f.serve(t)
	for _, ver := range []string{"0.3.1", "dev"} {
		u := testUpdater(t, ver, srv)
		tag, err := u.checkAndStage(context.Background())
		if err != nil || tag != "" {
			t.Errorf("version %s: checkAndStage = %q, %v; want nothing", ver, tag, err)
		}
	}
	if f.hits != 0 {
		t.Fatalf("downloaded %d times, want 0", f.hits)
	}
}

func TestInstallStagedSwapsApp(t *testing.T) {
	f := newFake(t, "v0.4.0", "new")
	u := testUpdater(t, "0.3.1", f.serve(t))
	if _, err := u.checkAndStage(context.Background()); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), appName+".app")
	bin := filepath.Join(root, "Contents/MacOS/caution-emacs")
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("old"), 0o755)

	ok, err := u.installStaged(root)
	if err != nil || !ok {
		t.Fatalf("installStaged = %v, %v", ok, err)
	}
	if b, _ := os.ReadFile(bin); string(b) != "new" {
		t.Fatalf("installed binary = %q, want new", b)
	}
	if _, err := os.Stat(root + ".old"); !os.IsNotExist(err) {
		t.Fatal("old app left behind")
	}
	if _, err := os.Stat(u.dir); !os.IsNotExist(err) {
		t.Fatal("update dir left behind")
	}
}

func TestInstallStagedIgnoresOlderStage(t *testing.T) {
	f := newFake(t, "v0.4.0", "new")
	u := testUpdater(t, "0.3.1", f.serve(t))
	if _, err := u.checkAndStage(context.Background()); err != nil {
		t.Fatal(err)
	}
	u.version = "0.4.0"
	root := filepath.Join(t.TempDir(), appName+".app")
	os.MkdirAll(root, 0o755)
	ok, err := u.installStaged(root)
	if err != nil || ok {
		t.Fatalf("installStaged = %v, %v; want no install", ok, err)
	}
}

func TestRestartFormMarksOnlyRealExit(t *testing.T) {
	form := restartForm("/tmp/x y/relaunch")
	want := `(run-at-time 0 nil (lambda () (let ((kill-emacs-hook (cons (lambda () (write-region "" nil "/tmp/x y/relaunch" nil 'silent)) kill-emacs-hook))) (save-buffers-kill-emacs))))`
	if form != want {
		t.Fatalf("restartForm =\n%s\nwant\n%s", form, want)
	}
}
