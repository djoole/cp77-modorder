// Package archive parses RDAR .archive files to extract file hash indexes.
package archive

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"golang.org/x/text/encoding/charmap"
)

// Archive represents a single .archive file and its internal file list.
type Archive struct {
	Name       string            // filename without directory, e.g. "my_mod.archive"
	Path       string            // absolute path on disk
	FileHashes []uint64          // FNV1a-64 hashes of all internal resources
	FilePaths  map[uint64]string // hash → resource path from LXRS footer; nil if absent
	HasXL      bool              // true when a same-name .xl file exists alongside the archive
}

// magic bytes at the start of a valid RDAR archive (Cyberpunk 2077).
var magic = [4]byte{'R', 'D', 'A', 'R'}

// Scan walks dir and parses every .archive file found in parallel.
// Non-fatal parse errors are logged to stderr and skipped.
// Results preserve the alphabetical order returned by os.ReadDir.
func Scan(dir string) ([]*Archive, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}

	xlNames := make(map[string]bool)
	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".archive":
			paths = append(paths, filepath.Join(dir, e.Name()))
		case ".xl":
			xlNames[strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))] = true
		}
	}
	archives := parseAll(paths)
	for _, a := range archives {
		a.HasXL = xlNames[strings.TrimSuffix(a.Name, filepath.Ext(a.Name))]
	}

	return archives, nil
}

// parseAll parses each path concurrently (up to runtime.NumCPU() at a time)
// and returns results in the same order as the input slice.
// Parse errors are logged to stderr; failed paths are omitted from the result.
func parseAll(paths []string) []*Archive {
	type indexed struct {
		idx int
		a   *Archive
	}

	out := make(chan indexed, len(paths))
	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup

	for i, p := range paths {
		wg.Add(1)
		i, p := i, p
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			a, err := parse(p)
			if err != nil {
				fmt.Fprintf(os.Stderr, "archive: skip %s: %v\n", filepath.Base(p), err)
				return
			}
			out <- indexed{i, a}
		}()
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	results := make([]indexed, 0, len(paths))
	for r := range out {
		results = append(results, r)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].idx < results[j].idx })

	archives := make([]*Archive, len(results))
	for i, r := range results {
		archives[i] = r.a
	}

	return archives
}

// ScanMO2 scans an MO2 instance mods directory. For each name in enabledMods it
// walks the corresponding subfolder recursively for .archive files and merges them
// into a single logical Archive (union of hashes) named after the mod folder.
// Mod folders are processed concurrently (up to runtime.NumCPU() at a time).
// Non-fatal parse errors are logged to stderr and skipped.
// Results preserve the order of enabledMods.
func ScanMO2(modsDir string, enabledMods []string) ([]*Archive, error) {
	type indexed struct {
		idx int
		a   *Archive
	}

	out := make(chan indexed, len(enabledMods))
	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup

	for i, modName := range enabledMods {
		wg.Add(1)
		i, modName := i, modName
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			modPath := filepath.Join(modsDir, modName)
			var found []*Archive
			var hasXL bool
			_ = filepath.WalkDir(modPath, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return nil // skip unreadable entries
				}
				if d.IsDir() {
					return nil
				}
				switch strings.ToLower(filepath.Ext(d.Name())) {
				case ".xl":
					hasXL = true
				case ".archive":
					a, parseErr := parse(path)
					if parseErr != nil {
						fmt.Fprintf(os.Stderr, "archive: MO2 skip %s: %v\n", path, parseErr)
						return nil
					}
					found = append(found, a)
				}
				return nil
			})
			if len(found) == 0 {
				return
			}
			merged := mergeArchives(modName, found)
			merged.HasXL = hasXL
			out <- indexed{i, merged}
		}()
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	results := make([]indexed, 0, len(enabledMods))
	for r := range out {
		results = append(results, r)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].idx < results[j].idx })

	archives := make([]*Archive, len(results))
	for i, r := range results {
		archives[i] = r.a
	}

	return archives, nil
}

// mergeArchives combines multiple archives from the same MO2 mod folder into one
// Archive. Name is the MO2 mod folder name; FileHashes and FilePaths are the union
// of all constituent archives.
func mergeArchives(name string, archives []*Archive) *Archive {
	seen := make(map[uint64]bool)
	merged := &Archive{
		Name:      name,
		Path:      archives[0].Path,
		FilePaths: make(map[uint64]string),
	}
	for _, a := range archives {
		for _, h := range a.FileHashes {
			if !seen[h] {
				seen[h] = true
				merged.FileHashes = append(merged.FileHashes, h)
			}
		}
		for k, v := range a.FilePaths {
			merged.FilePaths[k] = v
		}
	}
	if len(merged.FilePaths) == 0 {
		merged.FilePaths = nil
	}

	return merged
}

// parse reads the RED4 index from a single .archive file.
//
// Header (little-endian):
//
//	0x00  [4]byte  magic "RDAR"
//	0x04  uint32   version
//	0x08  uint64   indexOffset
//
// Index table at indexOffset:
//
//	0x00  uint32  fileTableOffset  (always 8)
//	0x04  uint32  fileTableSize
//	0x08  uint64  crc
//	0x10  uint32  fileEntryCount   ← actual file count
//	0x14  uint32  segmentCount
//	0x18  uint32  resourceDepCount
//
// Each FileEntry (56 bytes):
//
//	0x00  uint64    nameHash64
//	0x08  uint64    timestamp
//	0x10  uint32    numInlineBufferSegments
//	0x14  uint32    segmentsStart
//	0x18  uint32    segmentsEnd
//	0x1C  uint32    resourceDependenciesStart
//	0x20  uint32    resourceDependenciesEnd
//	0x24  [20]byte  sha1Hash
func parse(path string) (*Archive, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var hdr struct {
		Magic       [4]byte
		Version     uint32
		IndexOffset uint64
	}
	if err := binary.Read(f, binary.LittleEndian, &hdr); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	if hdr.Magic != magic {
		return nil, fmt.Errorf("not a RED4 archive (magic %x)", hdr.Magic)
	}

	if _, err := f.Seek(int64(hdr.IndexOffset), io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek to index: %w", err)
	}

	// Index table header — 28 bytes. FileEntryCount is at offset 0x10 within it.
	var idxHdr struct {
		FileTableOffset  uint32
		FileTableSize    uint32
		CRC              uint64
		FileEntryCount   uint32
		SegmentCount     uint32
		ResourceDepCount uint32
	}
	if err := binary.Read(f, binary.LittleEndian, &idxHdr); err != nil {
		return nil, fmt.Errorf("read index header: %w", err)
	}

	// Read each 56-byte FileEntry; only the name hash is needed.
	var entry struct {
		NameHash64                uint64
		Timestamp                 uint64
		NumInlineBufferSegments   uint32
		SegmentsStart             uint32
		SegmentsEnd               uint32
		ResourceDependenciesStart uint32
		ResourceDependenciesEnd   uint32
		SHA1Hash                  [20]byte
	}
	hashes := make([]uint64, 0, idxHdr.FileEntryCount)
	for i := uint32(0); i < idxHdr.FileEntryCount; i++ {
		if err := binary.Read(f, binary.LittleEndian, &entry); err != nil {
			return nil, fmt.Errorf("read file entry %d: %w", i, err)
		}
		hashes = append(hashes, entry.NameHash64)
	}

	filePaths := parseLXRS(f) // nil when absent, malformed, or not decompressible

	return &Archive{
		Name:       filepath.Base(path),
		Path:       path,
		FileHashes: hashes,
		FilePaths:  filePaths,
	}, nil
}

// parseLXRS attempts to read the optional SRXL resource-path footer. Archive
// tools call the structure LXRS, but its on-disk fourCC is "SRXL". Its string
// table is usually Oodle-compressed.
// Returns nil on any error — non-fatal, caller falls back to a hash database or
// ultimately to hexadecimal hashes.
func parseLXRS(f *os.File) map[uint64]string {
	const (
		footerSizeOffset = 0x28
		lxrsOffset       = 0xAC
		maxFooterSize    = 64 * 1024 * 1024
	)

	if _, err := f.Seek(footerSizeOffset, io.SeekStart); err != nil {
		return nil
	}
	var footerSize uint32
	if err := binary.Read(f, binary.LittleEndian, &footerSize); err != nil || footerSize < 20 || footerSize > maxFooterSize {
		return nil
	}

	if _, err := f.Seek(lxrsOffset, io.SeekStart); err != nil {
		return nil
	}

	var hdr struct {
		Magic          [4]byte
		Version        uint32
		RawSize        uint32
		CompressedSize uint32
		StringCount    uint32
	}
	if err := binary.Read(f, binary.LittleEndian, &hdr); err != nil {
		return nil
	}
	if hdr.Magic != [4]byte{'S', 'R', 'X', 'L'} || hdr.Version != 1 || hdr.StringCount == 0 ||
		hdr.RawSize == 0 || hdr.RawSize > maxFooterSize || hdr.CompressedSize == 0 ||
		hdr.CompressedSize > footerSize-20 {
		return nil
	}

	payload := make([]byte, hdr.CompressedSize)
	if _, err := io.ReadFull(f, payload); err != nil {
		return nil
	}
	data := payload
	if hdr.RawSize > hdr.CompressedSize {
		oodlePath := findGameOodle(filepath.Dir(f.Name()))
		if oodlePath == "" {
			return nil
		}
		var err error
		data, err = decompressOodle(payload, hdr.RawSize, oodlePath)
		if err != nil {
			return nil
		}
	} else if hdr.RawSize < hdr.CompressedSize {
		return nil
	}

	paths := make(map[uint64]string, hdr.StringCount)
	reader := strings.NewReader(string(data))
	for i := uint32(0); i < hdr.StringCount; i++ {
		s, err := readNullTermString(reader)
		if err != nil {
			break
		}
		if s != "" {
			decoded, err := charmap.ISO8859_1.NewDecoder().String(s)
			if err == nil {
				paths[fnv1a64(decoded)] = decoded
			}
		}
	}
	if len(paths) == 0 {
		return nil
	}

	return paths
}

// readNullTermString reads bytes until a null terminator or EOF.
func readNullTermString(r io.Reader) (string, error) {
	var buf []byte
	b := make([]byte, 1)
	for {
		if _, err := r.Read(b); err != nil {
			return "", err
		}
		if b[0] == 0 {
			return string(buf), nil
		}
		buf = append(buf, b[0])
	}
}

// fnv1a64 computes the FNV-1a 64-bit hash of s.
func fnv1a64(s string) uint64 {
	const (
		offset64 = uint64(14695981039346656037)
		prime64  = uint64(1099511628211)
	)
	h := offset64
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}

	return h
}
