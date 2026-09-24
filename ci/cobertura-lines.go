//go:build ignore

// cobertura-lines.go prints the line coverage of a Cobertura XML report as a
// percentage with one decimal, for ci/coverage-gate.sh. Files under cmd/
// (main() wiring) are not counted. Exits 3 on unreadable or malformed XML.
//
//	go run ./ci/cobertura-lines.go coverage/backend.xml
package main

import (
	"encoding/xml"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./ci/cobertura-lines.go REPORT.xml")
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		os.Exit(3)
	}
	var doc struct {
		XMLName xml.Name
		Classes []struct {
			Filename string `xml:"filename,attr"`
			Lines    []struct {
				Hits int `xml:"hits,attr"`
			} `xml:"lines>line"`
		} `xml:"packages>package>classes>class"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		os.Exit(3)
	}
	total, covered := 0, 0
	for _, c := range doc.Classes {
		if strings.HasPrefix(c.Filename, "cmd/") || strings.Contains(c.Filename, "/cmd/") {
			continue
		}
		for _, l := range c.Lines {
			total++
			if l.Hits > 0 {
				covered++
			}
		}
	}
	if total == 0 {
		fmt.Println("0.0")
		return
	}
	fmt.Printf("%.1f\n", 100*float64(covered)/float64(total))
}
