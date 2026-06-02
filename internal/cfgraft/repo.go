package cfgraft

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
)

func repoCachePath(paths Paths, id string, src Source) (string, error) {
	name := src.LocalID
	if name == "" {
		sum := sha256.Sum256([]byte(id + "\x00" + src.Repo))
		name = safeName(id) + "-" + hex.EncodeToString(sum[:])[:12]
	}
	clean := filepath.Clean(name)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("source %q local_id escapes repos directory", id)
	}
	return filepath.Join(paths.Repos, clean), nil
}

func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "source"
	}
	return b.String()
}

func isWithin(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
