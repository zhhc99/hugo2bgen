package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	inputBase  = "content/posts"
	assetsBase = "assets"
	outputBase = "output/content/posts"
)

type customDate struct {
	time.Time
}

func (d customDate) MarshalYAML() (any, error) {
	if d.IsZero() {
		return nil, nil
	}
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!timestamp",
		Value: d.Format("2006-01-02"),
	}, nil
}

// hugoFM represents every Hugo front-matter field we care to read.
// Date is parsed as any because Hugo allows both bare dates
// (2024-01-01, parsed as time.Time by yaml.v3) and quoted strings.
type hugoFM struct {
	Title   string   `yaml:"title"`
	Slug    string   `yaml:"slug"`
	Date    any      `yaml:"date"` // time.Time or string
	Author  string   `yaml:"author"`
	Summary string   `yaml:"summary"`
	Tags    []string `yaml:"tags"`
	Cover   struct {
		Image string `yaml:"image"`
	} `yaml:"cover"`
}

// bgenFM is what we write out.
// Date is time.Time so yaml.v3 emits a proper RFC3339 timestamp.
type bgenFM struct {
	Title   string     `yaml:"title,omitempty"`
	Slug    string     `yaml:"slug,omitempty"`
	Date    customDate `yaml:"date"`
	Author  string     `yaml:"author,omitempty"`
	Summary string     `yaml:"summary,omitempty"`
	Tags    []string   `yaml:"tags,omitempty"`
}

func main() {
	if _, err := os.Stat(inputBase); os.IsNotExist(err) {
		fatalf("input directory %q not found – run this from the Hugo project root\n", inputBase)
	}

	err := filepath.Walk(inputBase, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(inputBase, path)
		dst := filepath.Join(outputBase, rel)

		if info.IsDir() {
			return os.MkdirAll(dst, 0755)
		}

		if strings.HasSuffix(path, ".md") {
			return convertMarkdown(path, dst)
		}

		// Non-md files in bundles (images, etc.) – copy as-is.
		return copyFile(path, dst)
	})

	if err != nil {
		fatalf("error: %v\n", err)
	}
}

// convertMarkdown rewrites front-matter and copies the cover image if present.
func convertMarkdown(src, dst string) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	rewritten, coverSrc, coverDstName, err := rewriteFrontMatter(raw, src, dst)
	if err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(dst, rewritten, 0644); err != nil {
		return err
	}
	fmt.Printf("✓ %s\n", src)

	// Copy cover image if the front-matter had one.
	if coverSrc != "" {
		coverDst := filepath.Join(filepath.Dir(dst), coverDstName)
		if err := copyFile(coverSrc, coverDst); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: could not copy cover %s → %s: %v\n", coverSrc, coverDst, err)
		} else {
			fmt.Printf("  cover   %s → %s\n", coverSrc, coverDst)
		}
	}

	return nil
}

// rewriteFrontMatter parses Hugo YAML front-matter, emits bgen front-matter,
// and returns the source and destination filename for the cover image (if any).
//
// Bundle detection: if the markdown basename is "index.md" the post is a bundle.
//   - bundle:    cover dst = cover.<ext>
//   - bare file: cover dst = <markdown-basename>.<ext>
func rewriteFrontMatter(content []byte, srcPath, dstPath string) (
	out []byte, coverSrc, coverDstName string, err error,
) {
	s := string(content)
	if !strings.HasPrefix(s, "---") {
		return content, "", "", nil
	}

	afterOpen := strings.TrimPrefix(s[3:], "\r")
	afterOpen = strings.TrimPrefix(afterOpen, "\n")

	end := findClosingDelimiter(afterOpen)
	if end < 0 {
		return content, "", "", nil
	}

	fmRaw := afterOpen[:end]
	body := afterOpen[end:]
	body = strings.TrimPrefix(body, "---")

	var hugo hugoFM
	if err = yaml.Unmarshal([]byte(fmRaw), &hugo); err != nil {
		return nil, "", "", err
	}

	// --- Date: normalise to time.Time ---
	parsedDate, dateErr := toTime(hugo.Date)
	if dateErr != nil {
		return nil, "", "", fmt.Errorf("parsing date: %w", dateErr)
	}

	bgen := bgenFM{
		Title:   hugo.Title,
		Slug:    hugo.Slug,
		Date:    customDate{parsedDate},
		Author:  hugo.Author,
		Summary: hugo.Summary,
		Tags:    hugo.Tags,
	}

	// --- Cover image ---
	if hugo.Cover.Image != "" {
		coverSrc = filepath.Join(assetsBase, filepath.FromSlash(hugo.Cover.Image))
		ext := filepath.Ext(hugo.Cover.Image)

		isBundle := strings.EqualFold(filepath.Base(srcPath), "index.md")
		if isBundle {
			coverDstName = "cover" + ext
		} else {
			mdBase := strings.TrimSuffix(filepath.Base(dstPath), ".md")
			coverDstName = mdBase + ext
		}
	}

	// --- Serialise bgen front-matter ---
	var buf bytes.Buffer
	buf.WriteString("---\n")

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err = enc.Encode(bgen); err != nil {
		return nil, "", "", err
	}

	buf.WriteString("---")
	buf.WriteString(body)

	return buf.Bytes(), coverSrc, coverDstName, nil
}

// toTime converts whatever yaml.v3 gives us for a date field into time.Time.
// yaml.v3 parses bare YAML dates (2024-01-01) as time.Time automatically;
// quoted strings such as "2024-01-01" arrive as plain string.
func toTime(v any) (time.Time, error) {
	switch t := v.(type) {
	case time.Time:
		return t, nil
	case string:
		for _, layout := range []string{
			time.RFC3339,
			"2006-01-02T15:04:05",
			"2006-01-02",
		} {
			if parsed, err := time.Parse(layout, t); err == nil {
				return parsed, nil
			}
		}
		return time.Time{}, fmt.Errorf("unrecognised date format: %q", t)
	case nil:
		return time.Time{}, nil
	default:
		return time.Time{}, fmt.Errorf("unexpected date type %T", v)
	}
}

// findClosingDelimiter returns the byte offset in s where the closing ---
// line begins, or -1 if not found.
func findClosingDelimiter(s string) int {
	lines := strings.Split(s, "\n")
	offset := 0
	for i, line := range lines {
		if i == 0 {
			offset += len(line) + 1
			continue
		}
		if strings.TrimRight(line, "\r") == "---" {
			return offset
		}
		offset += len(line) + 1
	}
	return -1
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(1)
}
