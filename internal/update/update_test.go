package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var installer = []byte("installer bytes")

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// server answers as GitHub does for the latest release: tag, asset name and digest.
func server(t *testing.T, tag, asset, digest string, body []byte) Client {
	t.Helper()
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/repos/"+Repository+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q,"body":"notes","assets":[{"name":%q,"browser_download_url":%q,"digest":%q,"size":%d}]}`,
			tag, asset, base+"/download", digest, len(body))
	})
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) { w.Write(body) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	base = srv.URL
	return Client{API: srv.URL}
}

func TestFindsANewerRelease(t *testing.T) {
	c := server(t, "v0.2.0", "FreeView-0.2.0.exe", "sha256:"+digestOf(installer), installer)
	rel, ok, err := c.Latest(context.Background(), "0.1.2")
	if err != nil || !ok {
		t.Fatalf("Latest = %+v, %v, %v", rel, ok, err)
	}
	if rel.Version != "0.2.0" || rel.Notes != "notes" || rel.SHA256 != digestOf(installer) {
		t.Errorf("release = %+v", rel)
	}
}

func TestIgnoresReleasesThatAreNotNewerOrNotInstallable(t *testing.T) {
	good := "sha256:" + digestOf(installer)
	for name, c := range map[string]Client{
		"same version":       server(t, "v0.1.2", "FreeView-0.1.2.exe", good, installer),
		"older version":      server(t, "v0.1.0", "FreeView-0.1.0.exe", good, installer),
		"pre-release tag":    server(t, "v0.2.0-rc1", "FreeView-0.2.0-rc1.exe", good, installer),
		"other asset":        server(t, "v0.2.0", "appcast.xml", good, installer),
		"no digest":          server(t, "v0.2.0", "FreeView-0.2.0.exe", "", installer),
		"missing tag prefix": server(t, "0.2.0", "FreeView-0.2.0.exe", good, installer),
		"invalid digest":     server(t, "v0.2.0", "FreeView-0.2.0.exe", "sha256:"+strings.Repeat("z", 64), installer),
	} {
		if rel, ok, err := c.Latest(context.Background(), "0.1.2"); ok || err != nil {
			t.Errorf("%s: Latest = %+v, %v, %v", name, rel, ok, err)
		}
	}
}

func TestDownloadEnforcesReleaseSize(t *testing.T) {
	c := server(t, "v0.2.0", "FreeView-0.2.0.exe", "sha256:"+digestOf(installer), installer)
	rel, _, _ := c.Latest(context.Background(), "0.1.2")
	rel.Size--
	if _, err := c.Download(context.Background(), rel, t.TempDir()); err == nil {
		t.Fatal("accepted an installer larger than the release size")
	}
}

func TestLatestReportsGitHubErrors(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if _, ok, err := (Client{API: srv.URL}).Latest(context.Background(), "0.1.2"); ok || err == nil {
		t.Errorf("a 404 gave ok=%v err=%v", ok, err)
	}
}

func TestDownloadChecksTheDigest(t *testing.T) {
	dir := t.TempDir()
	c := server(t, "v0.2.0", "FreeView-0.2.0.exe", "sha256:"+digestOf(installer), installer)
	rel, _, _ := c.Latest(context.Background(), "0.1.2")
	path, err := c.Download(context.Background(), rel, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(installer) {
		t.Errorf("downloaded %q", got)
	}

	rel.SHA256 = digestOf([]byte("another"))
	if _, err := c.Download(context.Background(), rel, dir); err == nil {
		t.Error("a download that does not match its digest was accepted")
	}
	if got, err := os.ReadFile(filepath.Join(dir, "FreeView-0.2.0.exe")); err != nil || !bytes.Equal(got, installer) {
		t.Error("a failed download destroyed the previously verified installer")
	}
}

func TestCheckSchedule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "update-check")
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	if !Due(path, now) {
		t.Error("no file: a check is due")
	}
	if err := Mark(path, now); err != nil {
		t.Fatal(err)
	}
	for at, want := range map[time.Duration]bool{time.Hour: false, 23 * time.Hour: false, Interval: true, -time.Hour: true} {
		if got := Due(path, now.Add(at)); got != want {
			t.Errorf("Due after %v = %v, want %v", at, got, want)
		}
	}
}
