package picture

import (
	"bytes"
	"encoding/binary"
	"image/color"
	"runtime"
	"testing"
)

func TestMissingCodecErrors(t *testing.T) {
	if !missingCodec(hresult(0x88982f50)) || !missingCodec(hresult(0x88982f8b)) || missingCodec(hresult(0xc00d36be)) {
		t.Error("WIC's missing-codec errors are not told apart")
	}
}

// ddsBC1 builds an 8 × 4 DDS of two BC1 (DXT1) blocks: red on the left,
// blue on the right.
func ddsBC1() []byte {
	var b bytes.Buffer
	le := func(v any) { binary.Write(&b, binary.LittleEndian, v) }
	b.WriteString("DDS ")
	// DDS_HEADER: size, flags (caps, height, width, pixel format, linear
	// size), height, width, linear size, depth, mip count, reserved.
	le([]uint32{124, 0x1 | 0x2 | 0x4 | 0x1000 | 0x80000, 4, 8, 16, 0, 0})
	le(make([]uint32, 11))
	// DDS_PIXELFORMAT: size, DDPF_FOURCC, "DXT1", unused bit count and masks.
	le([]uint32{32, 0x4})
	b.WriteString("DXT1")
	le(make([]uint32, 5))
	le([]uint32{0x1000, 0, 0, 0, 0}) // DDSCAPS_TEXTURE, caps 2 to 4, reserved
	// Each block: colour 0 (RGB565), colour 1, then 2-bit indices.
	le([]uint16{0xf800, 0x001f})
	le(uint32(0)) // every texel colour 0, red
	le([]uint16{0xf800, 0x001f})
	le(uint32(0x55555555)) // every texel colour 1, blue
	return b.Bytes()
}

// Windows decodes DDS with its own codec, which ships with it.
func TestDDSIsDecodedByWindows(t *testing.T) {
	p, err := Load(ddsBC1(), "texture.dds")
	if err != nil {
		t.Fatal(err)
	}
	checkPixels(t, "dds", p, 8, 4, "dds", 1, 1, color.RGBA{255, 0, 0, 255}, 0)
	checkPixels(t, "dds", p, 8, 4, "dds", 6, 2, color.RGBA{0, 0, 255, 255}, 0)
	if _, err := Load(ddsBC1()[:100], "cut.dds"); err == nil {
		t.Error("a cut-off DDS decoded")
	}
}

var (
	createStreamOnHGlobal = ole32.NewProc("CreateStreamOnHGlobal")
	// GUID_ContainerFormatWmp (JPEG XR) and GUID_WICPixelFormat24bppBGR.
	containerFormatWmp = guid{0x57a37caa, 0x367a, 0x4540, [8]byte{0x91, 0x6b, 0xf1, 0x83, 0xc5, 0x09, 0x3a, 0x4b}}
	pixelFormatBGR     = guid{0x6fddc324, 0x4e03, 0x4bfe, [8]byte{0xb1, 0x85, 0x3d, 0x77, 0x76, 0x8d, 0xc9, 0x0c}}
)

// encodeWIC encodes a w × h 24-bit BGR image with the WIC encoder for
// container: Windows' own JPEG XR encoder writes the sample Load is
// tested on.
func encodeWIC(t *testing.T, container *guid, w, h int, bgr []byte) []byte {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if hr, _, _ := coInitializeEx.Call(0, coinitMultithreaded); hr == 0 || hr == 1 {
		defer coUninitialize.Call()
	}
	var factory, stream, encoder, frame, props com
	defer func() {
		for _, o := range []com{props, frame, encoder, stream, factory} {
			release(o)
		}
	}()
	must := func(err error, what string) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	hrErr := func(hr uintptr) error {
		if int32(hr) < 0 {
			return hresult(hr)
		}
		return nil
	}
	hr, _, _ := coCreateInstance.Call(ptr(&clsidWICImagingFactory), 0, clsctxInprocServer, ptr(&iidWICImagingFactory), ptr(&factory))
	must(hrErr(hr), "CoCreateInstance")
	hr, _, _ = createStreamOnHGlobal.Call(0, 1, ptr(&stream))
	must(hrErr(hr), "CreateStreamOnHGlobal")
	must(call(factory, 8, ptr(container), 0, ptr(&encoder)), "CreateEncoder")
	must(call(encoder, 3, uintptr(stream), 2), "IWICBitmapEncoder::Initialize") // WICBitmapEncoderNoCache
	must(call(encoder, 10, ptr(&frame), ptr(&props)), "CreateNewFrame")
	must(call(frame, 3, uintptr(props)), "IWICBitmapFrameEncode::Initialize")
	must(call(frame, 4, uintptr(w), uintptr(h)), "SetSize")
	format := pixelFormatBGR
	must(call(frame, 6, ptr(&format)), "SetPixelFormat")
	if format != pixelFormatBGR {
		t.Fatal("the encoder does not take 24-bit BGR")
	}
	must(call(frame, 10, uintptr(h), uintptr(w*3), uintptr(len(bgr)), ptr(&bgr[0])), "WritePixels")
	must(call(frame, 12), "IWICBitmapFrameEncode::Commit")
	must(call(encoder, 11), "IWICBitmapEncoder::Commit")
	// Read the encoded bytes back through the stream: its end is the
	// exact length written.
	var size uint64
	must(call(stream, 5, 0, 2, ptr(&size)), "IStream::Seek end") // STREAM_SEEK_END
	must(call(stream, 5, 0, 0, 0), "IStream::Seek start")
	if size == 0 {
		t.Fatal("empty encoder output")
	}
	out := make([]byte, size)
	var read uint32
	must(call(stream, 3, ptr(&out[0]), uintptr(size), ptr(&read)), "IStream::Read")
	return out[:read]
}

// Windows decodes JPEG XR with its own codec, under each of its names.
func TestJPEGXRIsDecodedByWindows(t *testing.T) {
	const w, h = 32, 16
	bgr := make([]byte, w*h*3)
	for y := range h {
		for x := range w {
			c := []byte{255, 0, 0} // blue, as BGR
			if x < w/2 {
				c = []byte{0, 160, 0} // green
			}
			copy(bgr[(y*w+x)*3:], c)
		}
	}
	data := encodeWIC(t, &containerFormatWmp, w, h, bgr)
	for _, name := range []string{"photo.jxr", "photo.wdp", "photo.hdp"} {
		p, err := Load(data, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		format := name[len(name)-3:]
		checkPixels(t, name, p, w, h, format, 4, 8, color.RGBA{0, 160, 0, 255}, 6)
		checkPixels(t, name, p, w, h, format, 28, 8, color.RGBA{0, 0, 255, 255}, 6)
	}
	if _, err := Load(data[:len(data)/3], "cut.jxr"); err == nil {
		t.Error("a cut-off JPEG XR decoded")
	}
}
