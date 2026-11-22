// Command appcast writes the signed appcast WinSparkle reads for the
// FreeView installer that tools/build_installer.py releases.
//
// nsgo build signs and lists the installer it compiles, but FreeView
// compiles that installer again with its license files and renames it,
// which changes its bytes and its name. So this signs the final installer
// with NSGO_UPDATES_KEY, as nsgo does, and writes the appcast in nsgo's
// format (cmd/nsgo/updates.go, appcastItem.feed, nsgo v0.2.0).
//
// Usage:
//
//	go run ./tools/appcast -installer FreeView-0.2.0.exe -version 0.2.0 \
//	  -repo OUBIGFA/FreeView -key <updates.key> -out appcast-windows_x64.xml
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"html"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

func main() {
	installer := flag.String("installer", "", "the installer to offer")
	version := flag.String("version", "", "its version, nsgo.yaml's")
	repo := flag.String("repo", "", "the GitHub repository whose v<version> release holds it")
	public := flag.String("key", "", "nsgo.yaml's updates.key")
	out := flag.String("out", "", "where to write the appcast")
	flag.Parse()
	if *installer == "" || *version == "" || *repo == "" || *public == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := write(*installer, *version, *repo, *public, *out); err != nil {
		log.Fatal(err)
	}
}

func decodeKey(text, what string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(text)
	if err != nil || len(b) != 32 {
		return nil, fmt.Errorf("%s is not a base64 32-byte key", what)
	}
	return b, nil
}

func write(installer, version, repo, public, out string) error {
	seed, err := decodeKey(os.Getenv("NSGO_UPDATES_KEY"), "NSGO_UPDATES_KEY")
	if err != nil {
		return err
	}
	pub, err := decodeKey(public, "-key")
	if err != nil {
		return err
	}
	key := ed25519.NewKeyFromSeed(seed)
	// As nsgo refuses: a key of another pair signs updates no app accepts.
	if !key.Public().(ed25519.PublicKey).Equal(ed25519.PublicKey(pub)) {
		return errors.New("NSGO_UPDATES_KEY: the secret key is not the one of updates.key")
	}
	data, err := os.ReadFile(installer)
	if err != nil {
		return err
	}
	tag := url.PathEscape("v" + version)
	name := filepath.Base(installer)
	x := html.EscapeString
	var b bytes.Buffer
	b.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n")
	b.WriteString("<rss version=\"2.0\" xmlns:sparkle=\"http://www.andymatuschak.org/xml-namespaces/sparkle\">\n  <channel>\n")
	b.WriteString("    <title>FreeView</title>\n    <item>\n")
	fmt.Fprintf(&b, "      <title>FreeView %s</title>\n", x(version))
	fmt.Fprintf(&b, "      <link>%s</link>\n", x("https://github.com/"+repo+"/releases/tag/"+tag))
	fmt.Fprintf(&b, "      <pubDate>%s</pubDate>\n", time.Now().UTC().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "      <sparkle:version>%s</sparkle:version>\n", x(version))
	fmt.Fprintf(&b, "      <sparkle:shortVersionString>%s</sparkle:shortVersionString>\n", x(version))
	// /S /RELAUNCH: nsgo's installer quits FreeView, installs quietly and
	// starts the new version.
	fmt.Fprintf(&b, "      <enclosure url=\"%s\" length=\"%d\" type=\"application/octet-stream\" sparkle:os=\"windows\" sparkle:version=\"%s\" sparkle:shortVersionString=\"%s\" sparkle:sha256=\"%x\" sparkle:edSignature=\"%s\" sparkle:installerArguments=\"/S /RELAUNCH\"/>\n",
		x("https://github.com/"+repo+"/releases/download/"+tag+"/"+url.PathEscape(name)), len(data), x(version), x(version),
		sha256.Sum256(data), base64.StdEncoding.EncodeToString(ed25519.Sign(key, data)))
	b.WriteString("    </item>\n  </channel>\n</rss>\n")
	return os.WriteFile(out, b.Bytes(), 0o644)
}
