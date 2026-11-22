// Package update finds a newer FreeView on GitHub, downloads its installer
// and starts it. The release holds the installer alone: its version is the
// release's tag, and the SHA-256 the download is checked against is the one
// GitHub states for the asset in its release API.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
)

// Repository is the GitHub repository whose releases carry the installers.
const Repository = "OUBIGFA/FreeView"

// Interval is how long a background check leaves between its checks.
const Interval = 24 * time.Hour

// Release is a newer version and its installer.
type Release struct {
	Version string
	Notes   string
	URL     string
	SHA256  string
	Size    int64
}

// Client reads releases from API, GitHub's API address unless a test sets another.
type Client struct {
	HTTP *http.Client
	API  string
}

func (c Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c Client) api() string {
	if c.API != "" {
		return strings.TrimRight(c.API, "/")
	}
	return "https://api.github.com"
}

type githubRelease struct {
	Tag    string `json:"tag_name"`
	Body   string `json:"body"`
	Assets []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest"`
		Size   int64  `json:"size"`
	} `json:"assets"`
}

// Latest reads the latest release, which GitHub keeps free of drafts and
// pre-releases. It reports false when that is not newer than current or has
// no installer named FreeView-<version>.exe with a SHA-256 digest.
func (c Client) Latest(ctx context.Context, current string) (Release, bool, error) {
	have, err := semver.NewVersion(current)
	if err != nil {
		return Release{}, false, fmt.Errorf("version %q: %w", current, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.api()+"/repos/"+Repository+"/releases/latest", nil)
	if err != nil {
		return Release{}, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.client().Do(req)
	if err != nil {
		return Release{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, false, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return Release{}, false, err
	}
	version, err := semver.StrictNewVersion(strings.TrimPrefix(rel.Tag, "v"))
	if err != nil || version.Prerelease() != "" || version.Metadata() != "" {
		return Release{}, false, nil
	}
	if !version.GreaterThan(have) {
		return Release{}, false, nil
	}
	name := "FreeView-" + version.String() + ".exe"
	for _, a := range rel.Assets {
		digest, ok := strings.CutPrefix(a.Digest, "sha256:")
		if a.Name == name && ok && len(digest) == 64 && a.Size > 0 {
			return Release{Version: version.String(), Notes: rel.Body, URL: a.URL, SHA256: digest, Size: a.Size}, true, nil
		}
	}
	return Release{}, false, nil
}

// Download saves the installer of r in dir and returns its path. A file
// whose SHA-256 differs from r's is removed and reported.
func (c Client) Download(ctx context.Context, r Release, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub answered %s", resp.Status)
	}
	path := filepath.Join(dir, "FreeView-"+r.Version+".exe")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o700)
	if err != nil {
		return "", err
	}
	sum := sha256.New()
	_, err = io.Copy(io.MultiWriter(file, sum), resp.Body)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil && !strings.EqualFold(hex.EncodeToString(sum.Sum(nil)), r.SHA256) {
		err = errors.New("the installer's SHA-256 does not match the release")
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

// Launch starts the installer silently. It quits the running FreeView,
// installs the new version and opens it again.
func Launch(path string) error {
	cmd := exec.Command(path, "/S", "/RELAUNCH")
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// Due reports whether the last check, whose time is kept in the file at
// path, is a day old or more. A missing or unreadable file, or a time ahead
// of now, makes a check due.
func Due(path string, now time.Time) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	last, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return true
	}
	since := now.Sub(last)
	return since < 0 || since >= Interval
}

// Mark keeps now as the time of the last check in the file at path.
func Mark(path string, now time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(now.UTC().Format(time.RFC3339)+"\n"), 0o600)
}
