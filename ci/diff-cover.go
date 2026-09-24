//go:build ignore

// diff-cover.go is the subset of diff-cover (the Python tool) that
// ci/patch-coverage.sh uses, in Go, so CI needs no Python:
//
//	go run ./ci/diff-cover.go REPORT --compare-branch REF --fail-under PCT --format markdown:OUT
//
// REPORT is Cobertura XML (paths are <source> joined with each class's
// filename) or an lcov tracefile (SF: paths as written). The lines this
// branch adds, `git diff -U0 $(merge-base REF HEAD)...HEAD`, are looked up in
// the report; a line counts when the report has it, and is covered when any
// entry for it has hits > 0. Lines the report does not have (comments,
// declarations, tests) are not counted, as in diff-cover.
//
// Exit 1 when the covered share is below --fail-under. When no changed line is
// in the report, the markdown says "No lines with coverage information" and it
// exits 0, which is what patch-coverage.sh's assert_matched keys on.
package main

import (
	"bufio"
	"encoding/xml"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./ci/diff-cover.go REPORT --compare-branch REF --fail-under PCT --format markdown:OUT")
		os.Exit(2)
	}
	report := os.Args[1]
	fs := flag.NewFlagSet("diff-cover", flag.ExitOnError)
	base := fs.String("compare-branch", "origin/master", "branch to compare against")
	failUnder := fs.Float64("fail-under", 0, "minimum covered percentage")
	format := fs.String("format", "", "markdown:PATH")
	_ = fs.Parse(os.Args[2:])

	lines, err := readReport(report)
	if err != nil {
		fmt.Fprintf(os.Stderr, "diff-cover: %s: %v\n", report, err)
		os.Exit(2)
	}
	changed, err := changedLines(*base)
	if err != nil {
		fmt.Fprintf(os.Stderr, "diff-cover: %v\n", err)
		os.Exit(2)
	}

	type fileResult struct {
		name           string
		total, covered int
		missing        []int
	}
	var results []fileResult
	total, covered := 0, 0
	for _, file := range sortedKeys(changed) {
		hits, ok := lines[file]
		if !ok {
			continue
		}
		r := fileResult{name: file}
		for _, ln := range changed[file] {
			h, ok := hits[ln]
			if !ok {
				continue
			}
			r.total++
			if h {
				r.covered++
			} else {
				r.missing = append(r.missing, ln)
			}
		}
		if r.total > 0 {
			results = append(results, r)
			total += r.total
			covered += r.covered
		}
	}

	var md strings.Builder
	md.WriteString("# Diff Coverage\n## Diff: " + *base + "...HEAD\n\n")
	pass := true
	if total == 0 {
		md.WriteString("No lines with coverage information in this diff.\n")
	} else {
		pct := 100 * float64(covered) / float64(total)
		pass = pct >= *failUnder
		for _, r := range results {
			fmt.Fprintf(&md, "- %s (%.1f%%)", r.name, 100*float64(r.covered)/float64(r.total))
			if len(r.missing) > 0 {
				fmt.Fprintf(&md, ": Missing lines %s", ranges(r.missing))
			}
			md.WriteString("\n")
		}
		fmt.Fprintf(&md, "\n## Summary\n\n- **Total**: %d lines\n- **Missing**: %d lines\n- **Coverage**: %d%%\n",
			total, total-covered, int(pct))
	}
	fmt.Print(md.String())
	if out, ok := strings.CutPrefix(*format, "markdown:"); ok {
		if err := os.WriteFile(out, []byte(md.String()), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "diff-cover: %v\n", err)
			os.Exit(2)
		}
	}
	if !pass {
		fmt.Fprintf(os.Stderr, "Failure. Coverage is below %v%%.\n", *failUnder)
		os.Exit(1)
	}
}

// readReport returns path -> line -> covered.
func readReport(file string) (map[string]map[int]bool, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	out := map[string]map[int]bool{}
	add := func(p string, ln int, hit bool) {
		p = path.Clean(p)
		if out[p] == nil {
			out[p] = map[int]bool{}
		}
		out[p][ln] = out[p][ln] || hit
	}
	if strings.HasPrefix(strings.TrimSpace(string(data)), "<") {
		var doc struct {
			Sources []string `xml:"sources>source"`
			Classes []struct {
				Filename string `xml:"filename,attr"`
				Lines    []struct {
					Number int `xml:"number,attr"`
					Hits   int `xml:"hits,attr"`
				} `xml:"lines>line"`
			} `xml:"packages>package>classes>class"`
		}
		if err := xml.Unmarshal(data, &doc); err != nil {
			return nil, err
		}
		root := ""
		if len(doc.Sources) > 0 {
			root = strings.TrimSpace(doc.Sources[0])
		}
		for _, c := range doc.Classes {
			for _, l := range c.Lines {
				add(path.Join(root, c.Filename), l.Number, l.Hits > 0)
			}
		}
		return out, nil
	}
	current := ""
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "SF:"):
			current = strings.TrimPrefix(line, "SF:")
		case strings.HasPrefix(line, "DA:") && current != "":
			parts := strings.Split(strings.TrimPrefix(line, "DA:"), ",")
			if len(parts) < 2 {
				continue
			}
			ln, err1 := strconv.Atoi(parts[0])
			hits, err2 := strconv.Atoi(parts[1])
			if err1 == nil && err2 == nil {
				add(current, ln, hits > 0)
			}
		}
	}
	return out, sc.Err()
}

// changedLines returns the added or changed line numbers per file, relative
// to the repo root, between the merge base with ref and HEAD.
func changedLines(ref string) (map[string][]int, error) {
	mb, err := exec.Command("git", "merge-base", ref, "HEAD").Output()
	if err != nil {
		return nil, fmt.Errorf("git merge-base %s HEAD: %w", ref, err)
	}
	diff, err := exec.Command("git", "diff", "--no-ext-diff", "--no-color", "--unified=0",
		strings.TrimSpace(string(mb))+"...HEAD").Output()
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	out := map[string][]int{}
	file := ""
	sc := bufio.NewScanner(strings.NewReader(string(diff)))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "+++ "):
			file = ""
			if p, ok := strings.CutPrefix(line, "+++ b/"); ok {
				file = path.Clean(p)
			}
		case strings.HasPrefix(line, "@@ ") && file != "":
			// @@ -a,b +c,d @@
			fields := strings.Fields(line)
			if len(fields) < 3 {
				continue
			}
			spec := strings.TrimPrefix(fields[2], "+")
			startS, countS, hasCount := strings.Cut(spec, ",")
			start, err := strconv.Atoi(startS)
			if err != nil {
				continue
			}
			count := 1
			if hasCount {
				if count, err = strconv.Atoi(countS); err != nil {
					continue
				}
			}
			for i := 0; i < count; i++ {
				out[file] = append(out[file], start+i)
			}
		}
	}
	return out, sc.Err()
}

func sortedKeys(m map[string][]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ranges renders sorted line numbers as "3-5,9".
func ranges(ns []int) string {
	var parts []string
	for i := 0; i < len(ns); {
		j := i
		for j+1 < len(ns) && ns[j+1] == ns[j]+1 {
			j++
		}
		if i == j {
			parts = append(parts, strconv.Itoa(ns[i]))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", ns[i], ns[j]))
		}
		i = j + 1
	}
	return strings.Join(parts, ",")
}
