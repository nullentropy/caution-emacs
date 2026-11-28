package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// set by scripts/release.sh with -ldflags "-X main.version=..."
var version = "dev"

const (
	appName     = "Caution Emacs"
	updateRepo  = "nullentropy/caution-emacs"
	updateAsset = "CautionEmacs-macos-universal.zip"
)

type updater struct {
	version string
	dir     string
	api, dl string
	client  *http.Client
}

func newUpdater(configDir string) updater {
	return updater{version: version, dir: filepath.Join(configDir, "update"),
		api: "https://api.github.com", dl: "https://github.com",
		client: &http.Client{Timeout: 10 * time.Minute}}
}

type staged struct {
	Tag     string `json:"tag"`
	Payload string `json:"payload"`
}

func parseTag(s string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func newerTag(cur, next string) bool {
	a, oka := parseTag(cur)
	b, okb := parseTag(next)
	if !oka || !okb {
		return false
	}
	for i := range 3 {
		if a[i] != b[i] {
			return b[i] > a[i]
		}
	}
	return false
}

func (u updater) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "caution-emacs-updater")
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp, nil
}

func (u updater) latestTag(ctx context.Context) (string, error) {
	resp, err := u.get(ctx, u.api+"/repos/"+updateRepo+"/releases/latest")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var r struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", fmt.Errorf("read the latest release: %w", err)
	}
	return r.Tag, nil
}

func (u updater) download(ctx context.Context, tag, name string) error {
	resp, err := u.get(ctx, u.dl+"/"+updateRepo+"/releases/download/"+tag+"/"+name)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	f, err := os.Create(filepath.Join(u.dir, name))
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (u updater) checkAndStage(ctx context.Context) (string, error) {
	if _, ok := parseTag(u.version); !ok {
		return "", nil
	}
	tag, err := u.latestTag(ctx)
	if err != nil {
		return "", err
	}
	if !newerTag(u.version, tag) {
		return "", nil
	}
	if s, err := u.readStaged(); err == nil && s.Tag == tag {
		if _, err := os.Stat(s.Payload); err == nil {
			return tag, nil
		}
	}
	if err := os.RemoveAll(u.dir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(u.dir, 0o700); err != nil {
		return "", err
	}
	payload, err := u.fetch(ctx, tag)
	if err != nil {
		os.RemoveAll(u.dir)
		return "", err
	}
	return tag, u.writeStaged(staged{Tag: tag, Payload: payload})
}

func (u updater) fetch(ctx context.Context, tag string) (string, error) {
	for _, name := range []string{updateAsset, "SHA256SUMS"} {
		if err := u.download(ctx, tag, name); err != nil {
			return "", err
		}
	}
	if err := verifySum(u.dir, updateAsset); err != nil {
		return "", err
	}
	if err := exec.CommandContext(ctx, "ditto", "-x", "-k", filepath.Join(u.dir, updateAsset), u.dir).Run(); err != nil {
		return "", fmt.Errorf("unpack %s: %w", updateAsset, err)
	}
	payload := filepath.Join(u.dir, appName+".app")
	if _, err := os.Stat(payload); err != nil {
		return "", fmt.Errorf("%s has no %s.app", updateAsset, appName)
	}
	return payload, nil
}

func verifySum(dir, asset string) error {
	sums, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return err
	}
	want := ""
	for _, line := range strings.Split(string(sums), "\n") {
		if f := strings.Fields(line); len(f) == 2 && strings.TrimPrefix(f[1], "*") == asset {
			want = f[0]
		}
	}
	if want == "" {
		return fmt.Errorf("the release has no checksum for %s", asset)
	}
	f, err := os.Open(filepath.Join(dir, asset))
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != want {
		return fmt.Errorf("%s does not match its checksum", asset)
	}
	return nil
}

func (u updater) stagedPath() string { return filepath.Join(u.dir, "staged.json") }

func (u updater) readStaged() (staged, error) {
	var s staged
	b, err := os.ReadFile(u.stagedPath())
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(b, &s)
}

func (u updater) writeStaged(s staged) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(u.stagedPath(), b, 0o600)
}

func (u updater) readyTag() string {
	s, err := u.readStaged()
	if err != nil || !newerTag(u.version, s.Tag) {
		return ""
	}
	return s.Tag
}

func (u updater) installStaged(root string) (bool, error) {
	s, err := u.readStaged()
	if err != nil {
		return false, nil
	}
	if !newerTag(u.version, s.Tag) {
		return false, os.RemoveAll(u.dir)
	}
	if err := swapInstall(root, s.Payload); err != nil {
		return false, err
	}
	os.RemoveAll(u.dir)
	return true, nil
}

func swapInstall(root, src string) error {
	old := root + ".old"
	os.RemoveAll(old)
	if err := os.Rename(root, old); err != nil {
		return fmt.Errorf("cannot replace %s: %w", root, err)
	}
	err := os.Rename(src, root)
	if err != nil {
		err = exec.Command("ditto", src, root).Run()
	}
	if err != nil {
		os.RemoveAll(root)
		if back := os.Rename(old, root); back != nil {
			return fmt.Errorf("update failed (%w) and %s could not be put back: %w", err, root, back)
		}
		return err
	}
	os.RemoveAll(old)
	return nil
}

// installRoot is the .app this binary runs from, or "" when it is not in one
func installRoot() (root, exe string) {
	exe, err := os.Executable()
	if err != nil {
		return "", ""
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", ""
	}
	root = filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if !strings.HasSuffix(root, ".app") {
		return "", exe
	}
	return root, exe
}

func relaunch(exe string) error {
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

// restartForm asks emacs to save and quit; the marker is written only if
// emacs really exits, so a cancelled prompt leaves no relaunch behind
func restartForm(marker string) string {
	return fmt.Sprintf(`(run-at-time 0 nil (lambda () (let ((kill-emacs-hook (cons (lambda () (write-region "" nil %q nil 'silent)) kill-emacs-hook))) (save-buffers-kill-emacs))))`, marker)
}

type updates struct {
	u         updater
	root, exe string
	marker    string

	stageMu sync.Mutex
	mu      sync.Mutex
	ready   string
	onReady func()
}

// startUpdates installs a staged update and relaunches, or starts the hourly
// check; it returns nil for builds that do not update themselves
func startUpdates(configDir string) *updates {
	if _, ok := parseTag(version); !ok {
		return nil
	}
	root, exe := installRoot()
	if root == "" {
		return nil
	}
	up := &updates{u: newUpdater(configDir), root: root, exe: exe,
		marker: filepath.Join(configDir, "relaunch")}
	os.Remove(up.marker)
	os.RemoveAll(root + ".old")
	if ok, err := up.u.installStaged(root); err != nil {
		log.Printf("update: %v", err)
	} else if ok {
		log.Printf("update: installed, relaunching")
		if err := relaunch(exe); err != nil {
			log.Printf("update: relaunch: %v", err)
		}
	}
	go up.watch()
	return up
}

func (up *updates) watch() {
	if tag := up.u.readyTag(); tag != "" {
		up.setReady(tag)
	}
	last := ""
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		_, err := up.check(ctx)
		cancel()
		if err != nil {
			if msg := err.Error(); msg != last {
				log.Printf("update check: %v", err)
				last = msg
			}
		} else {
			last = ""
		}
		time.Sleep(time.Hour)
	}
}

func (up *updates) check(ctx context.Context) (string, error) {
	up.stageMu.Lock()
	defer up.stageMu.Unlock()
	tag, err := up.u.checkAndStage(ctx)
	if err == nil && tag != "" {
		up.setReady(tag)
	}
	return tag, err
}

func (up *updates) setReady(tag string) {
	up.mu.Lock()
	changed := up.ready != tag
	up.ready = tag
	fn := up.onReady
	up.mu.Unlock()
	if changed && fn != nil {
		fn()
	}
}

func (up *updates) readyTag() string {
	if up == nil {
		return ""
	}
	up.mu.Lock()
	defer up.mu.Unlock()
	return up.ready
}

func (up *updates) setOnReady(fn func()) {
	up.mu.Lock()
	up.onReady = fn
	up.mu.Unlock()
}

// relaunchRequested reports whether emacs exited from Restart to Update
func (up *updates) relaunchRequested() bool {
	if up == nil {
		return false
	}
	_, err := os.Stat(up.marker)
	return err == nil
}

func (up *updates) installAndRelaunch() error {
	os.Remove(up.marker)
	ok, err := up.u.installStaged(up.root)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("no update is ready")
	}
	return relaunch(up.exe)
}
