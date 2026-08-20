//go:build !windows

package archive

import "fmt"

func decompressKark(_, _ string) ([]byte, error) {
	return nil, fmt.Errorf("KARK resource path resolution is only available on Windows")
}

func decompressOodle(_ []byte, _ uint32, _ string) ([]byte, error) {
	return nil, fmt.Errorf("Oodle decompression is only available on Windows")
}
