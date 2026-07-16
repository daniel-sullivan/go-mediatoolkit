//go:generate go run .

package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
)

const (
	module  = "github.com/daniel-sullivan/go-mediatoolkit"
	rawBase = "https://raw.githubusercontent.com/daniel-sullivan/go-mediatoolkit/main/"
)

// Fixed order for the Overview section overrides the usual path sort.
var overviewOrder = []string{"README.md", "BENCHMARKS.md", "LICENSING.md", "CONTRIBUTING.md"}

// Extra non-README docs included alongside the README.md walk.
var extraDocs = append(slices.Clone(overviewOrder[1:]), "codec/hwaccel/DESIGN.md")

var skipDirNames = map[string]bool{
	".git": true, "vendored": true, "internal": true, "testdata": true,
	"node_modules": true, "skills": true, ".claude-plugin": true,
}

const (
	secOverview   = "Overview"
	secCodecs     = "Audio codecs"
	secContainers = "Containers"
	secPipeline   = "Sample pipeline"
	secVideo      = "Video"
	secRuntime    = "Runtime and IO"
	secOther      = "Other"
	secOptional   = "Optional"
)

var sectionOrder = []string{secOverview, secCodecs, secContainers, secPipeline, secVideo, secRuntime, secOther, secOptional}

var audioCodecDirs = map[string]bool{"aac": true, "mp3": true, "flac": true, "opus": true, "pcm": true}

// Fixed order for the Video section: overview docs before the design doc.
// Also the membership list for classify.
var videoOrder = []string{"video/README.md", "codec/hwaccel/README.md", "codec/hwaccel/DESIGN.md"}

var pipelinePkgs = map[string]bool{
	"resample": true, "mutations": true, "loudness": true, "vad": true,
	"aec": true, "denoise": true, "duplex": true, "generators": true, "timeline": true, "mixer": true, "buffers": true,
}

var runtimePkgs = map[string]bool{
	"consts": true, "devices": true, "events": true, "inspection": true,
	"decode": true, "encode": true, "write": true,
}

// classify assigns a repo-relative doc path to an llms.txt section. Paths no
// rule recognises land in secOther so they are never dropped; run() warns
// when that happens so the maps above get extended.
func classify(path string) string {
	if slices.Contains(overviewOrder, path) {
		return secOverview
	}
	if slices.Contains(videoOrder, path) {
		return secVideo
	}
	parts := strings.Split(path, "/")
	switch {
	case len(parts) == 3 && parts[0] == "codec" && audioCodecDirs[parts[1]] && parts[2] == "README.md":
		return secCodecs
	case parts[0] == "containers":
		return secContainers
	case len(parts) == 2 && parts[1] == "README.md" && pipelinePkgs[parts[0]]:
		return secPipeline
	case parts[0] == "libraries":
		return secOptional
	case len(parts) == 2 && parts[1] == "README.md" && runtimePkgs[parts[0]]:
		return secRuntime
	default:
		return secOther
	}
}

type doc struct {
	path    string
	title   string
	desc    string // truncated, for link-list entries
	rawDesc string // untruncated, only populated for the root README
	content []byte // raw file bytes, inlined into llms-full.txt
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	check := flag.Bool("check", false, "verify llms.txt and llms-full.txt are up to date instead of writing them")
	flag.Parse()

	root, err := findRoot()
	if err != nil {
		return err
	}

	paths, err := discoverDocs(root)
	if err != nil {
		return err
	}

	docs := make(map[string]doc, len(paths))
	for _, p := range paths {
		content, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			return err
		}
		title, desc := parseDoc(string(content))
		// Package READMEs are titled by their directory path so entries are
		// unambiguous (libraries/aac vs codec/aac); their H1s are often bare
		// package names.
		if dir, ok := strings.CutSuffix(p, "/README.md"); ok {
			title = dir
		}
		if title == "" || title == "go-mediatoolkit" {
			title = p
		}
		if desc == "" {
			desc = p
		}
		d := doc{path: p, title: title, desc: truncateDesc(desc), content: content}
		if p == "README.md" {
			d.rawDesc = desc
		}
		docs[p] = d
	}

	sections := groupSections(paths, docs)
	for _, d := range sections[secOther] {
		fmt.Fprintf(os.Stderr, "warning: %s matched no section rule and was filed under %q — extend classify() in tools/llmsgen\n", d.path, secOther)
	}

	index := buildIndex(docs["README.md"].rawDesc, sections)

	pkgs, err := listPackages(root)
	if err != nil {
		return err
	}

	full, err := buildFull(root, docs["README.md"].rawDesc, sections, pkgs)
	if err != nil {
		return err
	}

	if *check {
		return checkFiles(root, index, full)
	}

	if err := os.WriteFile(filepath.Join(root, "llms.txt"), index, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "llms-full.txt"), full, 0o644)
}

// findRoot walks up from the working directory to the first directory
// containing go.mod.
func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found in any parent directory")
		}
		dir = parent
	}
}

// discoverDocs walks the repo for README.md files (skipping the fenced
// directories) and adds the fixed set of extra docs, returning a sorted,
// deduplicated list of repo-relative paths.
func discoverDocs(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipDirNames[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "README.md" {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool, len(paths))
	for _, p := range paths {
		seen[p] = true
	}
	for _, extra := range extraDocs {
		if _, err := os.Stat(filepath.Join(root, extra)); err != nil {
			return nil, fmt.Errorf("required doc %s: %w", extra, err)
		}
		if !seen[extra] {
			paths = append(paths, extra)
			seen[extra] = true
		}
	}

	sort.Strings(paths)
	return paths, nil
}

var (
	badgeLine = regexp.MustCompile(`\[!\[`)
	// Link/image URLs may contain one level of balanced parentheses
	// (e.g. wikipedia.org/wiki/Noise_(disambiguation)).
	imgLink  = regexp.MustCompile(`!\[[^\]]*\]\((?:[^()]|\([^()]*\))*\)`)
	mdLink   = regexp.MustCompile(`\[([^\]]*)\]\((?:[^()]|\([^()]*\))*\)`)
	whitespc = regexp.MustCompile(`\s+`)
)

// parseDoc extracts the H1 title and the first prose paragraph after it from
// a README's raw content, skipping badge-only lines.
func parseDoc(content string) (title, desc string) {
	lines := strings.Split(content, "\n")

	i := 0
	for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "# ") {
		i++
	}
	if i == len(lines) {
		return "", ""
	}
	title = clean(strings.TrimPrefix(strings.TrimSpace(lines[i]), "# "))
	i++

	for i < len(lines) {
		line := strings.TrimSpace(lines[i])
		if line == "" || badgeLine.MatchString(line) {
			i++
			continue
		}
		break
	}

	var para []string
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			break
		}
		if badgeLine.MatchString(line) {
			i++
			continue
		}
		para = append(para, line)
		i++
	}

	return title, clean(strings.Join(para, " "))
}

// clean strips images entirely, markdown links (to their text), bold/italic
// emphasis, code backticks, and collapses whitespace.
func clean(s string) string {
	s = imgLink.ReplaceAllString(s, "")
	s = mdLink.ReplaceAllString(s, "$1")
	s = strings.ReplaceAll(s, "*", "")
	s = strings.ReplaceAll(s, "`", "")
	s = whitespc.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	// A paragraph that introduces a list ends in ":"; as a standalone
	// description that reads dangling.
	if rest, ok := strings.CutSuffix(s, ":"); ok {
		s = rest + "."
	}
	return s
}

// truncateDesc cuts s at the first sentence end at or past 160 runes, hard
// capping at 250 runes with an ellipsis if no sentence end is found first.
func truncateDesc(s string) string {
	const softLimit, hardLimit = 160, 250
	r := []rune(s)
	if len(r) <= softLimit {
		return s
	}
	for i := softLimit; i < len(r) && i < hardLimit; i++ {
		if (r[i] == '.' || r[i] == '!' || r[i] == '?') && (i+1 == len(r) || r[i+1] == ' ') {
			return string(r[:i+1])
		}
	}
	if len(r) <= hardLimit {
		return s
	}
	cut := r[:hardLimit]
	if idx := strings.LastIndex(string(cut), " "); idx > 0 {
		cut = []rune(string(cut)[:idx])
	}
	return string(cut) + "…"
}

// groupSections buckets docs into their llms.txt sections, in section order,
// sorted by path within each section except Overview which keeps its fixed
// order.
func groupSections(paths []string, docs map[string]doc) map[string][]doc {
	sections := make(map[string][]doc, len(sectionOrder))
	for _, p := range paths {
		sec := classify(p)
		sections[sec] = append(sections[sec], docs[p])
	}
	for _, sec := range sectionOrder {
		list := sections[sec]
		switch sec {
		case secOverview:
			sort.Slice(list, func(i, j int) bool {
				return slices.Index(overviewOrder, list[i].path) < slices.Index(overviewOrder, list[j].path)
			})
		case secVideo:
			sort.Slice(list, func(i, j int) bool {
				return slices.Index(videoOrder, list[i].path) < slices.Index(videoOrder, list[j].path)
			})
		default:
			sort.Slice(list, func(i, j int) bool { return list[i].path < list[j].path })
		}
	}
	return sections
}

const fixedNote = `This is go-mediatoolkit (module ` + "`" + module + "`" + `), a pure-Go audio and
video toolkit. Every compressed audio codec (AAC, MP3, FLAC, Opus) is a
bit-exact or byte-identical 1:1 port of its canonical C reference, and the
default build is ` + "`CGO_ENABLED=0`" + ` pure Go, with the C references available as
opt-in cgo backends. This file follows the llms.txt convention
(https://llmstxt.org). The companion file ` + "`llms-full.txt`" + `, alongside this one
at the repository root, inlines every documentation file plus a generated Go
API reference (` + "`go doc -all`" + ` for every public package). Installable agent
skills for this toolkit are also available via the Claude Code plugin
marketplace hosted in this repository:
` + "`/plugin marketplace add daniel-sullivan/go-mediatoolkit`" + `.`

func rawURL(path string) string {
	return rawBase + path
}

func buildIndex(summary string, sections map[string][]doc) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "# go-mediatoolkit\n\n> %s\n\n%s\n\n", summary, fixedNote)

	for _, sec := range sectionOrder {
		list := sections[sec]
		if len(list) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n", sec)
		for _, d := range list {
			fmt.Fprintf(&b, "- [%s](%s): %s\n", d.title, rawURL(d.path), d.desc)
		}
		b.WriteString("\n")
	}

	return append(bytes.TrimRight(b.Bytes(), "\n"), '\n')
}

const fullNote = `Generated by tools/llmsgen (` + "`mise run llms`" + `) — do not edit by hand.

This file has two parts: Part 1 inlines every documentation file in this
repository (the same set indexed by ` + "`llms.txt`" + `); Part 2 is a generated Go
API reference (` + "`go doc -all`" + ` for every public, non-internal package).`

func delimiter(source string) string {
	rule := strings.Repeat("-", 80)
	return rule + "\nSource: " + source + "\n" + rule + "\n"
}

func buildFull(root, summary string, sections map[string][]doc, pkgs []string) ([]byte, error) {
	var b bytes.Buffer
	fmt.Fprintf(&b, "# go-mediatoolkit — full documentation for LLMs\n\n> %s\n\n%s\n\n", summary, fullNote)

	b.WriteString("# Part 1 — documentation\n\n")
	for _, sec := range sectionOrder {
		for _, d := range sections[sec] {
			b.WriteString(delimiter(fmt.Sprintf("%s (%s)", d.path, rawURL(d.path))))
			b.Write(bytes.TrimRight(d.content, "\n"))
			b.WriteString("\n\n")
		}
	}

	b.WriteString("# Part 2 — API reference (go doc -all)\n\n")
	// go doc shells out once per package; run them concurrently, emit in order.
	outs := make([][]byte, len(pkgs))
	errs := make([]error, len(pkgs))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, pkg := range pkgs {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			outs[i], errs[i] = goDoc(root, pkg)
		})
	}
	wg.Wait()
	for i, pkg := range pkgs {
		if errs[i] != nil {
			return nil, fmt.Errorf("go doc -all %s: %w", pkg, errs[i])
		}
		b.WriteString(delimiter("go doc -all " + pkg))
		b.Write(bytes.TrimRight(outs[i], "\n"))
		b.WriteString("\n\n")
	}

	return append(bytes.TrimRight(b.Bytes(), "\n"), '\n'), nil
}

// listPackages runs `go list` under CGO_ENABLED=0 and returns the sorted
// import paths eligible for the API reference: no package main, nothing
// under /internal/ or /vendored/, and nothing under libraries/ or tools/.
func listPackages(root string) ([]string, error) {
	cmd := exec.Command("go", "list", "-f", "{{.ImportPath}} {{.Name}}", "./...")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list ./...: %w", err)
	}

	var pkgs []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		importPath, name, ok := strings.Cut(line, " ")
		if !ok || name == "main" {
			continue
		}
		if strings.Contains(importPath, "/internal/") || strings.Contains(importPath, "/vendored/") {
			continue
		}
		rel := strings.TrimPrefix(importPath, module+"/")
		if strings.HasPrefix(rel, "libraries/") || strings.HasPrefix(rel, "tools/") {
			continue
		}
		pkgs = append(pkgs, importPath)
	}

	sort.Strings(pkgs)
	return pkgs, nil
}

func goDoc(root, importPath string) ([]byte, error) {
	cmd := exec.Command("go", "doc", "-all", importPath)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, stderr.String())
	}
	return out, nil
}

func checkFiles(root string, index, full []byte) error {
	var stale []string
	if drifted(root, "llms.txt", index) {
		stale = append(stale, "llms.txt")
	}
	if drifted(root, "llms-full.txt", full) {
		stale = append(stale, "llms-full.txt")
	}
	if len(stale) == 0 {
		return nil
	}
	return fmt.Errorf("%s out of date — run `mise run llms` and commit the result", strings.Join(stale, ", "))
}

func drifted(root, name string, want []byte) bool {
	got, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		return true
	}
	return !bytes.Equal(got, want)
}
