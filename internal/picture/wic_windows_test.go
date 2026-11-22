package picture

import "testing"

func TestMissingCodecErrors(t *testing.T) {
	if !missingCodec(hresult(0x88982f50)) || !missingCodec(hresult(0x88982f8b)) || missingCodec(hresult(0xc00d36be)) {
		t.Error("WIC's missing-codec errors are not told apart")
	}
}
