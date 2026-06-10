package skillzip

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/open-ma/oma-building/internal/store"
)

const (
	maxTotalUncompressed = 100 * 1024 * 1024
	maxFileUncompressed  = 25 * 1024 * 1024
	maxFileCount         = 500
)

// ParsedSkillZip is the normalized skill payload from a packaged zip.
type ParsedSkillZip struct {
	Files       []store.SkillFileInput
	Name        string
	Description string
}

var ignoredBasenames = map[string]struct{}{
	".DS_Store": {},
	"Thumbs.db": {},
}

var ignoredPrefixes = []string{
	"__MACOSX/",
	".git/",
	".idea/",
	".vscode/",
}

// ParseSkillZip reads a skill package zip and returns file inputs matching
// the JSON POST /v1/skills shape.
func ParseSkillZip(data []byte) (ParsedSkillZip, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ParsedSkillZip{}, fmt.Errorf(
			"Could not read zip: %s", err.Error(),
		)
	}

	type entry struct {
		path  string
		bytes []byte
	}
	var usable []entry
	var totalUncompressed int64

	for _, file := range reader.File {
		if file.FileInfo().IsDir() || zipEntryIgnored(file.Name) {
			continue
		}
		if len(usable) >= maxFileCount {
			return ParsedSkillZip{}, fmt.Errorf(
				"Zip has too many files (>%d); refusing to process",
				maxFileCount,
			)
		}
		size := file.UncompressedSize64
		if size == 0 {
			size = uint64(file.UncompressedSize)
		}
		if size > maxFileUncompressed {
			return ParsedSkillZip{}, fmt.Errorf(
				"File %q exceeds per-file limit", file.Name,
			)
		}
		totalUncompressed += int64(size)
		if totalUncompressed > maxTotalUncompressed {
			return ParsedSkillZip{}, fmt.Errorf(
				"Zip uncompressed size exceeds limit (zip-bomb defense)",
			)
		}

		rc, err := file.Open()
		if err != nil {
			return ParsedSkillZip{}, fmt.Errorf(
				"Could not read zip: %s", err.Error(),
			)
		}
		body, err := io.ReadAll(io.LimitReader(rc, maxFileUncompressed+1))
		_ = rc.Close()
		if err != nil {
			return ParsedSkillZip{}, fmt.Errorf(
				"Could not read zip: %s", err.Error(),
			)
		}
		if int64(len(body)) > maxFileUncompressed {
			return ParsedSkillZip{}, fmt.Errorf(
				"File %q exceeds per-file limit", file.Name,
			)
		}
		usable = append(usable, entry{
			path:  path.Clean(strings.ReplaceAll(file.Name, "\\", "/")),
			bytes: body,
		})
	}

	if len(usable) == 0 {
		return ParsedSkillZip{}, fmt.Errorf(
			"Zip is empty (after filtering metadata files)",
		)
	}

	paths := make([]string, len(usable))
	for i, e := range usable {
		paths[i] = e.path
	}
	prefix := commonRootPrefix(paths)

	var skillMDText string
	var skillMDPath string
	files := make([]store.SkillFileInput, 0, len(usable))
	for _, entry := range usable {
		rel := entry.path
		if prefix != "" {
			rel = strings.TrimPrefix(rel, prefix)
		}
		if rel == "" {
			continue
		}
		if strings.EqualFold(rel, "skill.md") {
			text, ok := tryDecodeUTF8(entry.bytes)
			if !ok {
				return ParsedSkillZip{}, fmt.Errorf(
					"SKILL.md must be UTF-8 text",
				)
			}
			skillMDText = text
			skillMDPath = rel
		}
	}

	if skillMDText == "" {
		return ParsedSkillZip{}, fmt.Errorf(
			"Zip must contain SKILL.md at the root " +
				"(or a single top-level folder containing it)",
		)
	}

	for _, entry := range usable {
		rel := entry.path
		if prefix != "" {
			rel = strings.TrimPrefix(rel, prefix)
		}
		if rel == "" {
			continue
		}
		var content string
		var encoding string
		if rel == skillMDPath {
			content = skillMDText
			encoding = "utf8"
		} else if text, ok := tryDecodeUTF8(entry.bytes); ok {
			content = text
			encoding = "utf8"
		} else {
			content = base64.StdEncoding.EncodeToString(entry.bytes)
			encoding = "base64"
		}
		files = append(files, store.SkillFileInput{
			Filename: rel,
			Content:  content,
			Encoding: encoding,
		})
	}

	name, description := parseFrontmatter(skillMDText)
	return ParsedSkillZip{
		Files:       files,
		Name:        name,
		Description: description,
	}, nil
}

func zipEntryIgnored(entryPath string) bool {
	clean := path.Clean(strings.ReplaceAll(entryPath, "\\", "/"))
	for _, p := range ignoredPrefixes {
		if strings.HasPrefix(clean, p) ||
			strings.Contains(clean, "/"+p) {
			return true
		}
	}
	base := path.Base(clean)
	if _, ok := ignoredBasenames[base]; ok {
		return true
	}
	if strings.HasPrefix(base, "._") {
		return true
	}
	return false
}

func commonRootPrefix(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	firstSlash := strings.Index(paths[0], "/")
	if firstSlash < 0 {
		return ""
	}
	candidate := paths[0][:firstSlash+1]
	for _, p := range paths[1:] {
		if !strings.HasPrefix(p, candidate) {
			return ""
		}
	}
	return candidate
}

func tryDecodeUTF8(data []byte) (string, bool) {
	if !utf8.Valid(data) {
		return "", false
	}
	return string(data), true
}

func parseFrontmatter(content string) (name, description string) {
	lines := strings.Split(content, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return "", ""
	}
	meta := map[string]string{}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			break
		}
		parts := strings.SplitN(lines[i], ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, `"'`)
		meta[key] = val
	}
	return meta["name"], meta["description"]
}
