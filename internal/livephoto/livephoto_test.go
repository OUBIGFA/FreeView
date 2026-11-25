package livephoto

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
)

// mp4Box builds an ISO BMFF box of type typ around payload.
func mp4Box(typ string, payload []byte) []byte {
	b := binary.BigEndian.AppendUint32(nil, uint32(8+len(payload)))
	return append(append(b, typ...), payload...)
}

// testMP4 is a minimal MP4 with the major brand brand: ftyp, moov and mdat.
func testMP4(brand string) []byte {
	var b []byte
	b = append(b, mp4Box("ftyp", []byte(brand+"\x00\x00\x02\x00isomiso2mp41"))...)
	b = append(b, mp4Box("moov", mp4Box("mvhd", make([]byte, 100)))...)
	b = append(b, mp4Box("mdat", bytes.Repeat([]byte{0xab}, 300))...)
	return b
}

// testJPEG is a minimal JPEG: SOI, an APP1 XMP segment when xmp is not
// empty, a few bytes of image data and EOI.
func testJPEG(xmp string) []byte {
	b := []byte{0xff, 0xd8}
	if xmp != "" {
		seg := append([]byte("http://ns.adobe.com/xap/1.0/\x00"), xmp...)
		b = append(b, 0xff, 0xe1)
		b = binary.BigEndian.AppendUint16(b, uint16(2+len(seg)))
		b = append(b, seg...)
	}
	b = append(b, 0xff, 0xda, 0x00, 0x02, 1, 2, 3, 4, 5, 6, 7, 8)
	return append(b, 0xff, 0xd9)
}

func microVideoXMP(offset int) string {
	return fmt.Sprintf(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF><rdf:Description `+
		`xmlns:GCamera="http://ns.google.com/photos/1.0/camera/" GCamera:MicroVideo="1" `+
		`GCamera:MicroVideoVersion="1" GCamera:MicroVideoOffset="%d"/></rdf:RDF></x:xmpmeta>`, offset)
}

func directoryXMP(length, padding int) string {
	return fmt.Sprintf(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF><rdf:Description GCamera:MotionPhoto="1">`+
		`<Container:Directory><rdf:Seq>`+
		`<rdf:li rdf:parseType="Resource"><Container:Item Item:Mime="image/jpeg" Item:Semantic="Primary" Item:Length="0" Item:Padding="0"/></rdf:li>`+
		`<rdf:li rdf:parseType="Resource"><Container:Item Item:Mime="video/mp4" Item:Semantic="MotionPhoto" Item:Length="%d" Item:Padding="%d"/></rdf:li>`+
		`</rdf:Seq></Container:Directory></rdf:Description></rdf:RDF></x:xmpmeta>`, length, padding)
}

// checkClip checks that FindVideo finds exactly want in file, as a
// subslice of file rather than a copy.
func checkClip(t *testing.T, file, want []byte, ext string) {
	t.Helper()
	v, ok := FindVideo(file)
	if !ok {
		t.Fatal("no clip found")
	}
	if !bytes.Equal(v.Data, want) {
		t.Fatalf("clip is %d bytes, want %d", len(v.Data), len(want))
	}
	if v.Ext != ext {
		t.Errorf("Ext = %q, want %q", v.Ext, ext)
	}
	if &v.Data[0] != &file[bytes.LastIndex(file, want)] {
		t.Error("clip is not a subslice of the input")
	}
}

func TestMicroVideoOffset(t *testing.T) {
	clip := testMP4("isom")
	file := append(testJPEG(microVideoXMP(len(clip))), clip...)
	checkClip(t, file, clip, ".mp4")
}

func TestContainerDirectory(t *testing.T) {
	clip := testMP4("mp42")
	file := append(testJPEG(directoryXMP(len(clip), 0)), clip...)
	checkClip(t, file, clip, ".mp4")
}

func TestContainerDirectoryPadding(t *testing.T) {
	clip := testMP4("mp42")
	file := append(testJPEG(directoryXMP(len(clip), 4)), clip...)
	file = append(file, 0, 0, 0, 0)
	checkClip(t, file, clip, ".mp4")
}

func TestOffsetCorrected(t *testing.T) {
	clip := testMP4("isom")
	for _, delta := range []int{-37, 50, 1000} {
		file := append(testJPEG(microVideoXMP(len(clip)+delta)), clip...)
		checkClip(t, file, clip, ".mp4")
	}
}

func TestSamsungTrailer(t *testing.T) {
	clip := testMP4("mp42")
	// Samsung writes a MotionPhoto_Data block before the clip and an
	// SEFH ... SEFT directory after it; its XMP gives no length.
	var file []byte
	file = append(file, testJPEG(`<x:xmpmeta><rdf:Description GCamera:MotionPhoto="1"/></x:xmpmeta>`)...)
	file = append(file, 0, 0, 0x30, 0x0a, 0x10, 0, 0, 0)
	file = append(file, "MotionPhoto_Data"...)
	file = append(file, clip...)
	file = append(file, "SEFH\x6b\x00\x00\x00\x01\x00\x00\x00\x00\x00\x30\x0a"...)
	file = append(file, "\x00\x00\x00\x00\x20\x00\x00\x00SEFT"...)
	checkClip(t, file, clip, ".mp4")

	// The same with a trailer that happens to look like a box header
	// running past the end of the file.
	file = append(file[:len(file)-28], "\x7f\xff\xff\xffSEFHxxxxxxxx"...)
	checkClip(t, file, clip, ".mp4")
}

func TestMarkerWithoutLength(t *testing.T) {
	clip := testMP4("qt  ")
	file := append(testJPEG(`<x:xmpmeta><rdf:Description GCamera:MicroVideo="1"/></x:xmpmeta>`), clip...)
	checkClip(t, file, clip, ".mov")
}

func TestHEIFXMP(t *testing.T) {
	clip := testMP4("isom")
	var file []byte
	file = append(file, mp4Box("ftyp", []byte("heic\x00\x00\x00\x00mif1heic"))...)
	file = append(file, mp4Box("meta", []byte(microVideoXMP(len(clip))))...)
	file = append(file, mp4Box("mdat", make([]byte, 64))...)
	file = append(file, clip...)
	checkClip(t, file, clip, ".mp4")
}

func TestLargeSize(t *testing.T) {
	clip := testMP4("isom")
	// mdat, the last box, rewritten with a 64-bit size.
	mdat := bytes.Index(clip, []byte("mdat")) - 4
	payload := clip[mdat+8:]
	big := append([]byte(nil), clip[:mdat]...)
	big = append(big, 0, 0, 0, 1)
	big = append(big, "mdat"...)
	big = binary.BigEndian.AppendUint64(big, uint64(16+len(payload)))
	big = append(big, payload...)
	file := append(testJPEG(microVideoXMP(len(big))), big...)
	checkClip(t, file, big, ".mp4")

	// And with size 0, running to the end of the file.
	zero := append([]byte(nil), clip...)
	binary.BigEndian.PutUint32(zero[mdat:], 0)
	file = append(testJPEG(microVideoXMP(len(zero))), zero...)
	checkClip(t, file, zero, ".mp4")
}

func TestOrdinaryPhotos(t *testing.T) {
	heic := append(mp4Box("ftyp", []byte("heic\x00\x00\x00\x00mif1heic")), mp4Box("meta", make([]byte, 40))...)
	heic = append(heic, mp4Box("moov", make([]byte, 8))...) // as an image sequence has
	heic = append(heic, mp4Box("mdat", make([]byte, 64))...)
	for name, file := range map[string][]byte{
		"jpeg":             testJPEG(""),
		"jpeg with xmp":    testJPEG(`<x:xmpmeta><rdf:Description xmp:Rating="3"/></x:xmpmeta>`),
		"jpeg with mp4":    append(testJPEG(""), testMP4("isom")...), // no marker: not a motion photo
		"heic":             heic,
		"heic with marker": append(append([]byte(nil), heic...), "MotionPhoto_Data"...),
		"empty":            nil,
	} {
		if v, ok := FindVideo(file); ok {
			t.Errorf("%s: found a %d-byte clip", name, len(v.Data))
		}
	}
}

func TestCorrupt(t *testing.T) {
	clip := testMP4("isom")
	moov := bytes.Index(clip, []byte("moov")) - 4
	mdat := bytes.Index(clip, []byte("mdat")) - 4
	mutate := func(f func(b []byte) []byte) []byte { return f(append([]byte(nil), clip...)) }
	cases := map[string][]byte{
		"truncated":       clip[:len(clip)-10],
		"no moov":         append(clip[:moov:moov], clip[mdat:]...),
		"huge moov":       mutate(func(b []byte) []byte { binary.BigEndian.PutUint32(b[moov:], 0xffffff00); return b }),
		"tiny moov":       mutate(func(b []byte) []byte { binary.BigEndian.PutUint32(b[moov:], 4); return b }),
		"huge ftyp":       mutate(func(b []byte) []byte { binary.BigEndian.PutUint32(b, 0x7fffffff); return b }),
		"short ftyp":      mutate(func(b []byte) []byte { binary.BigEndian.PutUint32(b, 8); return b }),
		"largesize short": append(clip[:mdat:mdat], 0, 0, 0, 1, 'm', 'd', 'a', 't', 0, 0),
		"largesize huge": mutate(func(b []byte) []byte {
			b = append(b[:mdat], 0, 0, 0, 1, 'm', 'd', 'a', 't')
			return binary.BigEndian.AppendUint64(b, 1<<63)
		}),
		"only ftyp": clip[:12],
	}
	for name, body := range cases {
		for _, xmp := range []string{microVideoXMP(len(body)), `<x:xmpmeta GCamera:MotionPhoto="1"/>`,
			directoryXMP(len(body), 0), microVideoXMP(1 << 50), directoryXMP(-5, 9)} {
			file := append(testJPEG(xmp), body...)
			if v, ok := FindVideo(file); ok {
				t.Errorf("%s: found a %d-byte clip", name, len(v.Data))
			}
		}
	}
	// Every prefix of a motion photo: no panic, and never a clip with a
	// box cut short. A cut between moov and mdat leaves a complete
	// ftyp and moov, which is accepted; a cut inside mdat is not.
	file := append(testJPEG(microVideoXMP(len(clip))), clip...)
	mdatStart := len(file) - len(clip) + mdat
	for n := range len(file) {
		v, ok := FindVideo(file[:n])
		if ok && (n >= mdatStart+8 || len(v.Data) != mdat) {
			t.Fatalf("prefix %d: found a %d-byte clip", n, len(v.Data))
		}
	}
}

func TestIsLIVP(t *testing.T) {
	for name, want := range map[string]bool{"a.livp": true, `x\IMG_1.LIVP`: true, "a.heic": false, "livp": false} {
		if got := IsLIVP(name); got != want {
			t.Errorf("IsLIVP(%q) = %v", name, got)
		}
	}
}

func makeZip(t *testing.T, method uint16, files map[string][]byte, order []string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range order {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestOpenLIVP(t *testing.T) {
	heic := append(mp4Box("ftyp", []byte("heic\x00\x00\x00\x00mif1heic")), mp4Box("mdat", make([]byte, 50))...)
	mov := testMP4("qt  ")
	files := map[string][]byte{"IMG_0001.HEIC": heic, "IMG_0001.MOV": mov, "__MACOSX/._IMG_0001.HEIC": []byte("fork")}
	order := []string{"__MACOSX/._IMG_0001.HEIC", "IMG_0001.MOV", "IMG_0001.HEIC"}
	for _, method := range []uint16{zip.Store, zip.Deflate} {
		data := makeZip(t, method, files, order)
		still, name, clip, err := OpenLIVP(data)
		if err != nil {
			t.Fatal(err)
		}
		if name != "IMG_0001.HEIC" || !bytes.Equal(still, heic) {
			t.Errorf("method %d: still %q, %d bytes", method, name, len(still))
		}
		if clip.Ext != ".mov" || !bytes.Equal(clip.Data, mov) {
			t.Errorf("method %d: clip %q, %d bytes", method, clip.Ext, len(clip.Data))
		}
		if method == zip.Store && &still[0] != &data[bytes.Index(data, heic)] {
			t.Error("stored still is not a subslice of the input")
		}
	}

	onlyClip := makeZip(t, zip.Store, map[string][]byte{"a.mov": mov}, []string{"a.mov"})
	if _, _, _, err := OpenLIVP(onlyClip); !errors.Is(err, ErrNoStill) {
		t.Errorf("without a still: %v", err)
	}
	if _, _, _, err := OpenLIVP([]byte("not a zip")); err == nil {
		t.Error("not a zip: no error")
	}
}
