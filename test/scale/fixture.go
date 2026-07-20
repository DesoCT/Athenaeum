// Package scale generates a large synthetic workspace and measures the
// non-functional targets N1, N2, and N3 against it.
//
// The corpus is generated rather than committed: 5,000 documents totalling
// 2 GB do not belong in a repository, and a deterministic generator reproduces
// them exactly on any machine.
package scale

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
)

// Corpus describes a generated workspace.
type Corpus struct {
	Root string
	// Documents is the number of Markdown files.
	Documents int
	// Bytes is the total size actually written.
	Bytes int64
}

// GenerateOptions control corpus generation.
type GenerateOptions struct {
	Root string
	// Documents is how many Markdown files to write.
	Documents int
	// TargetBytes is the total body size to aim for, spread across documents.
	TargetBytes int64
	// Seed makes generation deterministic, so a rerun measures the same corpus.
	Seed int64
}

// vocabulary is drawn from the specification's own subject matter, so search
// queries in the measurements hit realistic term frequencies rather than
// uniformly random noise.
var vocabulary = strings.Fields(`
workspace document markdown render editor preview conflict recovery watcher
index projection cache disposable session annotation note relationship asset
git status diff blame heading outline slug front matter fence code block table
task list footnote callout mermaid math sanitise policy origin token loopback
remote bind atomic write rename fsync fingerprint version stale boundary guard
traversal symlink exclude include glob pattern group pinned recent command
palette keyboard focus contrast landmark portable static binary embedded
concurrency worker pool cancellation debounce coalesce batch transaction reader
writer latency throughput corpus fixture acceptance requirement constitution
`)

// Generate writes a synthetic workspace, reusing one that already exists.
//
// Regenerating two gigabytes on every run would dominate the measurement, so a
// corpus whose document count already matches is left alone.
func Generate(opts GenerateOptions) (*Corpus, error) {
	if opts.Documents <= 0 {
		return nil, fmt.Errorf("a corpus needs at least one document")
	}
	docsDir := filepath.Join(opts.Root, "docs")

	if existing, err := count(docsDir); err == nil && existing == opts.Documents {
		size, err := totalSize(docsDir)
		if err != nil {
			return nil, err
		}
		return &Corpus{Root: opts.Root, Documents: existing, Bytes: size}, nil
	}

	if err := os.RemoveAll(opts.Root); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(opts.Root, "athenaeum.toml"),
		[]byte(configTOML), 0o644); err != nil {
		return nil, err
	}

	perDocument := opts.TargetBytes / int64(opts.Documents)
	random := rand.New(rand.NewSource(opts.Seed))

	var written int64
	for i := range opts.Documents {
		// A shallow directory tree, as a real corpus has: a flat directory of
		// 5,000 entries is not what the file tree or the watcher will meet.
		dir := filepath.Join(docsDir, fmt.Sprintf("area-%02d", i%40), fmt.Sprintf("part-%02d", (i/40)%25))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		body := document(random, i, perDocument)
		path := filepath.Join(dir, fmt.Sprintf("doc-%05d.md", i))
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return nil, err
		}
		written += int64(len(body))
	}

	return &Corpus{Root: opts.Root, Documents: opts.Documents, Bytes: written}, nil
}

// document builds one synthetic Markdown file of roughly size bytes.
func document(random *rand.Rand, index int, size int64) string {
	var b strings.Builder

	// Every twentieth document carries front matter with tags, so the
	// front-matter and tag paths are exercised at scale too.
	if index%20 == 0 {
		fmt.Fprintf(&b, "---\ntitle: Synthetic document %d\ntags:\n  - %s\n  - %s\n---\n\n",
			index, word(random), word(random))
	}
	fmt.Fprintf(&b, "# Synthetic document %d\n\n", index)

	section := 0
	for int64(b.Len()) < size {
		section++
		fmt.Fprintf(&b, "## Section %d: %s %s\n\n", section, word(random), word(random))

		for range 2 + random.Intn(3) {
			b.WriteString(paragraph(random, 30+random.Intn(50)))
			b.WriteString("\n\n")
		}

		switch section % 4 {
		case 1:
			fmt.Fprintf(&b, "```go\nfunc %s() error {\n\treturn nil // %s\n}\n```\n\n",
				word(random), word(random))
		case 2:
			for range 3 {
				fmt.Fprintf(&b, "- %s\n", paragraph(random, 6))
			}
			b.WriteString("\n")
		case 3:
			fmt.Fprintf(&b, "See [document %d](../../area-%02d/part-%02d/doc-%05d.md).\n\n",
				(index+1)%5000, (index+1)%40, ((index+1)/40)%25, (index+1)%5000)
		}

		// A rare needle, so a measured query can match exactly one document and
		// the timing is not dominated by result-set size.
		if section == 3 {
			fmt.Fprintf(&b, "Marker token zqx%05d for exact-match measurement.\n\n", index)
		}
	}
	return b.String()
}

func paragraph(random *rand.Rand, words int) string {
	parts := make([]string, 0, words)
	for range words {
		parts = append(parts, word(random))
	}
	sentence := strings.Join(parts, " ")
	return strings.ToUpper(sentence[:1]) + sentence[1:] + "."
}

func word(random *rand.Rand) string {
	return vocabulary[random.Intn(len(vocabulary))]
}

func count(dir string) (int, error) {
	total := 0
	err := filepath.WalkDir(dir, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			total++
		}
		return nil
	})
	return total, err
}

func totalSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}

const configTOML = `
schema_version = 1
name = "Scale Fixture"
root = "."

include = ["docs/**/*.md"]
exclude = ["**/node_modules/**", ".git/**"]

[search]
enabled = true
index_code_blocks = true
index_front_matter = true

[git]
enabled = false

[security]
writable = ["docs/**/*.md"]

[[groups]]
id = "area-00"
title = "Area 00"
patterns = ["docs/area-00/**/*.md"]
`
