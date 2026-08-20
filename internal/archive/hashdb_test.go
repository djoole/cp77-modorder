package archive

import (
	"encoding/binary"
	"os"
	"testing"
)

func TestResolvePathBuffer(t *testing.T) {
	wantedPath := `base\quest\example.ent`
	wantedHash := fnv1a64(wantedPath)
	targets := map[uint64]struct{}{
		wantedHash:           {},
		fnv1a64("not-found"): {},
	}

	resolved := resolvePathBuffer([]byte("base\\other.mesh\r\n"+wantedPath+"\r\n"), targets)
	if got := resolved[wantedHash]; got != wantedPath {
		t.Fatalf("resolved path = %q, want %q", got, wantedPath)
	}
	if len(resolved) != 1 {
		t.Fatalf("resolved %d paths, want 1", len(resolved))
	}
	if len(targets) != 2 {
		t.Fatalf("ResolvePathBuffer mutated its targets map")
	}
}

func TestParseLXRSUncompressed(t *testing.T) {
	const path = `base\quest\example.questphase`
	payload := append([]byte(path), 0)
	footerSize := uint32(20 + len(payload))
	archiveData := make([]byte, 0xAC+int(footerSize))
	binary.LittleEndian.PutUint32(archiveData[0x28:0x2C], footerSize)
	copy(archiveData[0xAC:0xB0], []byte("SRXL"))
	binary.LittleEndian.PutUint32(archiveData[0xB0:0xB4], 1)
	binary.LittleEndian.PutUint32(archiveData[0xB4:0xB8], uint32(len(payload)))
	binary.LittleEndian.PutUint32(archiveData[0xB8:0xBC], uint32(len(payload)))
	binary.LittleEndian.PutUint32(archiveData[0xBC:0xC0], 1)
	copy(archiveData[0xC0:], payload)

	file, err := os.CreateTemp(t.TempDir(), "*.archive")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write(archiveData); err != nil {
		t.Fatal(err)
	}

	paths := parseLXRS(file)
	if got := paths[fnv1a64(path)]; got != path {
		t.Fatalf("resolved path = %q, want %q", got, path)
	}
}
