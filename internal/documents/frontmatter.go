package documents

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// Front matter formats (R3, spec 03 section 4).
const (
	FrontMatterNone = "none"
	FrontMatterYAML = "yaml"
	FrontMatterTOML = "toml"
)

var (
	yamlFence = []byte("---")
	tomlFence = []byte("+++")
)

// frontMatter is the parsed prelude of a document.
type frontMatter struct {
	// Format is one of the FrontMatter* constants.
	Format string
	// Fields holds the decoded key/value pairs. Nil when there is none.
	Fields map[string]any
	// BodyOffset is the byte offset where the document body starts.
	BodyOffset int
	// BodyLine is the 1-based source line where the body starts, so heading
	// line numbers stay correct relative to the whole file.
	BodyLine int
	// Err records a parse failure. Malformed front matter must not make a
	// document unreadable: the body still renders and the problem is surfaced.
	Err error
}

// parseFrontMatter detects and decodes a leading front matter block.
//
// enabled lists the formats the workspace permits (documents.front_matter).
// A block written in a disabled format is left as body content.
func parseFrontMatter(source []byte, enabled []string) frontMatter {
	none := frontMatter{Format: FrontMatterNone, BodyOffset: 0, BodyLine: 1}

	fence, format := detectFence(source)
	if fence == nil || !formatEnabled(format, enabled) {
		return none
	}

	// The opening fence must be the whole first line.
	firstLineEnd := bytes.IndexByte(source, '\n')
	if firstLineEnd < 0 {
		return none
	}
	if !bytes.Equal(bytes.TrimRight(source[:firstLineEnd], "\r"), fence) {
		return none
	}

	// Find the closing fence on a line of its own.
	rest := source[firstLineEnd+1:]
	closeOffset, closeLen := findClosingFence(rest, fence)
	if closeOffset < 0 {
		// An unterminated block is not front matter; treat the whole file as
		// body rather than swallowing it.
		return none
	}

	raw := rest[:closeOffset]
	bodyOffset := firstLineEnd + 1 + closeOffset + closeLen

	fm := frontMatter{
		Format:     format,
		BodyOffset: bodyOffset,
		BodyLine:   bytes.Count(source[:bodyOffset], []byte("\n")) + 1,
		Fields:     map[string]any{},
	}

	switch format {
	case FrontMatterYAML:
		if err := yaml.Unmarshal(raw, &fm.Fields); err != nil {
			fm.Err = fmt.Errorf("front matter is not valid YAML: %w", err)
			fm.Fields = map[string]any{}
		}
	case FrontMatterTOML:
		if err := toml.Unmarshal(raw, &fm.Fields); err != nil {
			fm.Err = fmt.Errorf("front matter is not valid TOML: %w", err)
			fm.Fields = map[string]any{}
		}
	}
	return fm
}

func detectFence(source []byte) ([]byte, string) {
	switch {
	case bytes.HasPrefix(source, yamlFence):
		return yamlFence, FrontMatterYAML
	case bytes.HasPrefix(source, tomlFence):
		return tomlFence, FrontMatterTOML
	default:
		return nil, FrontMatterNone
	}
}

func formatEnabled(format string, enabled []string) bool {
	for _, e := range enabled {
		if strings.EqualFold(e, format) {
			return true
		}
	}
	return false
}

// findClosingFence returns the offset of the closing fence within rest, and the
// number of bytes the fence line occupies including its newline.
func findClosingFence(rest, fence []byte) (offset, length int) {
	pos := 0
	for pos <= len(rest) {
		lineEnd := bytes.IndexByte(rest[pos:], '\n')
		var line []byte
		var consumed int
		if lineEnd < 0 {
			line = rest[pos:]
			consumed = len(line)
		} else {
			line = rest[pos : pos+lineEnd]
			consumed = lineEnd + 1
		}

		if bytes.Equal(bytes.TrimRight(line, "\r"), fence) {
			return pos, consumed
		}
		if lineEnd < 0 {
			return -1, 0
		}
		pos += consumed
	}
	return -1, 0
}

// Title extracts a title field from front matter, if present and a string.
func (f frontMatter) Title() string {
	if f.Fields == nil {
		return ""
	}
	if v, ok := f.Fields["title"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}
