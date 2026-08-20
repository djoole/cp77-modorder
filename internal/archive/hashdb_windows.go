//go:build windows

package archive

import (
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var oodleProcs sync.Map

func decompressKark(databasePath, oodlePath string) ([]byte, error) {
	compressed, err := os.ReadFile(databasePath)
	if err != nil {
		return nil, err
	}
	if len(compressed) < 9 || string(compressed[:4]) != "KARK" {
		return nil, fmt.Errorf("invalid KARK database")
	}

	rawSize := binary.LittleEndian.Uint32(compressed[4:8])
	if rawSize == 0 || rawSize > 512*1024*1024 {
		return nil, fmt.Errorf("invalid KARK output size %d", rawSize)
	}

	return decompressOodle(compressed[8:], rawSize, oodlePath)
}

func decompressOodle(payload []byte, rawSize uint32, oodlePath string) ([]byte, error) {
	if len(payload) == 0 || rawSize == 0 {
		return nil, fmt.Errorf("empty Oodle payload")
	}

	raw := make([]byte, int(rawSize))
	procValue, _ := oodleProcs.LoadOrStore(oodlePath, windows.NewLazyDLL(oodlePath).NewProc("OodleLZ_Decompress"))
	proc := procValue.(*windows.LazyProc)
	result, _, callErr := proc.Call(
		uintptr(unsafe.Pointer(&payload[0])),
		uintptr(len(payload)),
		uintptr(unsafe.Pointer(&raw[0])),
		uintptr(len(raw)),
		1, // fuzzSafe
		0, // checkCRC
		0, // verbosity
		0, // decBufBase
		0, // decBufSize
		0, // callback
		0, // callback user data
		0, // decoder memory
		0, // decoder memory size
		3, // all thread phases
	)
	runtime.KeepAlive(payload)
	runtime.KeepAlive(raw)
	if int64(result) != int64(rawSize) {
		return nil, fmt.Errorf("Oodle decompression failed: result %d (%v)", int64(result), callErr)
	}

	return raw, nil
}
