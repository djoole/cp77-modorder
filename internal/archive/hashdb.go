package archive

import (
	"bytes"
	"os"
	"path/filepath"
)

// ResolveKnownPaths looks up resource hashes in WolvenKit's usedhashes.kark
// database when Cyber Engine Tweaks has installed it in the game directory.
// Resolution is optional: an unavailable or unreadable database simply returns
// an empty map so callers can continue displaying hexadecimal hashes.
func ResolveKnownPaths(scanDir string, targets map[uint64]struct{}) map[uint64]string {
	resolved := make(map[uint64]string)
	if len(targets) == 0 {
		return resolved
	}

	databasePath, oodlePath := findHashDatabase(scanDir)
	if databasePath == "" || oodlePath == "" {
		return resolved
	}

	data, err := decompressKark(databasePath, oodlePath)
	if err != nil {
		return resolved
	}

	return resolvePathBuffer(data, targets)
}

func findHashDatabase(scanDir string) (string, string) {
	oodlePath := findGameOodle(scanDir)
	if oodlePath == "" {
		return "", ""
	}
	gameDir := filepath.Dir(filepath.Dir(filepath.Dir(oodlePath)))
	databasePath := filepath.Join(gameDir, "bin", "x64", "plugins", "cyber_engine_tweaks", "tweakdb", "usedhashes.kark")
	if !fileExists(databasePath) {
		return "", ""
	}

	return databasePath, oodlePath
}

func findGameOodle(startDir string) string {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return ""
	}

	for {
		oodlePath := filepath.Join(dir, "bin", "x64", "oo2ext_7_win64.dll")
		if fileExists(oodlePath) {
			return oodlePath
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)

	return err == nil && !info.IsDir()
}

func resolvePathBuffer(data []byte, targets map[uint64]struct{}) map[uint64]string {
	remaining := make(map[uint64]struct{}, len(targets))
	for hash := range targets {
		remaining[hash] = struct{}{}
	}

	resolved := make(map[uint64]string)
	for len(data) > 0 && len(remaining) > 0 {
		line := data
		if newline := bytes.IndexByte(data, '\n'); newline >= 0 {
			line = data[:newline]
			data = data[newline+1:]
		} else {
			data = nil
		}

		line = bytes.TrimSuffix(line, []byte{'\r'})
		if len(line) == 0 {
			continue
		}

		hash := fnv1a64Bytes(line)
		if _, wanted := remaining[hash]; !wanted {
			continue
		}

		resolved[hash] = string(line)
		delete(remaining, hash)
	}

	return resolved
}

func fnv1a64Bytes(data []byte) uint64 {
	const (
		offset64 = uint64(14695981039346656037)
		prime64  = uint64(1099511628211)
	)

	hash := offset64
	for _, b := range data {
		hash ^= uint64(b)
		hash *= prime64
	}

	return hash
}
