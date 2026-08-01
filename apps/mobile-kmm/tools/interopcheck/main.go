// Command interopcheck validates the Swift↔Kotlin boundary without a Swift
// compiler.
//
// # Why this exists
//
// Kotlin/Native compiles Apple targets on any host, so `iosMain` is checked by
// `make test-mobile`. Linking the framework and compiling Swift both need
// Xcode, which is not available on every machine that touches this repository —
// so roughly 2,500 lines of SwiftUI go unverified between Mac builds, and the
// errors they accumulate are overwhelmingly of one kind: a reference to a
// Kotlin symbol that does not exist, or exists with different parameter labels.
//
// That class is mechanically checkable. This reads the Kotlin declarations out
// of `commonMain`, reads the Swift call sites out of `iosApp`, and reports
// every reference on a Kotlin-typed receiver that does not resolve.
//
// # What it does not do
//
// It is not a type checker and does not try to be. It will not catch a wrong
// argument *type*, a SwiftUI misuse, or a logic error, and it does not model
// generics or inheritance. It catches misspelled and imagined API — which,
// having just found four compile errors sitting in `iosMain` for want of anyone
// running the compiler, is the failure that actually happens.
//
// A finding is a definite problem. A clean run is not a promise that the Swift
// compiles.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ── the Kotlin side ──────────────────────────────────────────────────────────

type kotlinType struct {
	name string
	// members is every property and function name the type exposes, plus the
	// enum entries when it is an enum. Lower-cased for enum entries, because
	// that is what Kotlin/Native's ObjC export does to them.
	members map[string]bool
	// funcs maps a function name to the ordered parameter names, which become
	// Swift argument labels.
	funcs  map[string][]string
	isEnum bool
}

var (
	reType = regexp.MustCompile(
		`(?m)^\s*(?:public\s+|internal\s+)?(?:data\s+|sealed\s+|value\s+)?(class|interface|object|enum class)\s+([A-Z]\w*)`)
	reFunc       = regexp.MustCompile(`(?m)^\s*(?:override\s+)?(?:suspend\s+)?fun\s+(?:<[^>]+>\s*)?(\w+)\s*\(([^)]*)\)`)
	reProperty   = regexp.MustCompile(`(?m)^\s*(?:override\s+)?(?:va[lr])\s+(\w+)\s*[:=]`)
	reCtorProp   = regexp.MustCompile(`(?m)^\s*(?:@\w+(?:\([^)]*\))?\s+)*(?:va[lr])\s+(\w+)\s*:`)
	reEnumEntry  = regexp.MustCompile(`(?m)^\s{4}([A-Z][A-Za-z0-9_]*)\s*[,;]`)
	reDataObject = regexp.MustCompile(`(?m)^\s*data object\s+([A-Z]\w*)`)
)

func scanKotlin(roots []string) map[string]*kotlinType {
	types := map[string]*kotlinType{}

	get := func(name string) *kotlinType {
		if t, ok := types[name]; ok {
			return t
		}
		t := &kotlinType{name: name, members: map[string]bool{}, funcs: map[string][]string{}}
		types[name] = t
		return t
	}

	for _, root := range roots {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".kt") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			src := string(raw)

			// Declarations are attributed to the nearest preceding type, which
			// is crude and adequate: these files are one public type each, and
			// a member attributed to the wrong sibling only ever *widens* what
			// the checker accepts. It never invents a false finding.
			decls := reType.FindAllStringSubmatchIndex(src, -1)
			for i, d := range decls {
				kind := src[d[2]:d[3]]
				name := src[d[4]:d[5]]
				end := len(src)
				if i+1 < len(decls) {
					end = decls[i+1][0]
				}
				body := src[d[1]:end]
				t := get(name)
				t.isEnum = t.isEnum || kind == "enum class"

				for _, m := range reFunc.FindAllStringSubmatch(body, -1) {
					t.members[m[1]] = true
					t.funcs[m[1]] = paramNames(m[2])
				}
				for _, m := range reProperty.FindAllStringSubmatch(body, -1) {
					t.members[m[1]] = true
				}
				for _, m := range reCtorProp.FindAllStringSubmatch(body, -1) {
					t.members[m[1]] = true
				}
				if t.isEnum {
					for _, m := range reEnumEntry.FindAllStringSubmatch(body, -1) {
						// Kotlin/Native lower-cases enum entries on export.
						t.members[strings.ToLower(m[1])] = true
					}
				}
			}

			// Sealed hierarchy members become SKIE enum cases, lower-cased.
			for _, m := range reDataObject.FindAllStringSubmatch(src, -1) {
				for _, t := range types {
					t.members[strings.ToLower(m[1])] = true
				}
			}
			return nil
		})
	}
	return types
}

func paramNames(sig string) []string {
	var out []string
	depth := 0
	field := strings.Builder{}
	flush := func() {
		p := strings.TrimSpace(field.String())
		field.Reset()
		if p == "" {
			return
		}
		// "name: Type = default" → name
		if i := strings.Index(p, ":"); i > 0 {
			p = strings.TrimSpace(p[:i])
		}
		p = strings.TrimPrefix(p, "vararg ")
		p = strings.Trim(p, "`")
		if p != "" {
			out = append(out, p)
		}
	}
	for _, r := range sig {
		switch r {
		case '<', '(', '[':
			depth++
		case '>', ')', ']':
			depth--
		case ',':
			if depth == 0 {
				flush()
				continue
			}
		}
		field.WriteRune(r)
	}
	flush()
	return out
}

// ── the Swift side ───────────────────────────────────────────────────────────

var (
	// `let viewModel: FeedViewModel`, `@State private var current: SellUiState?`
	// `let viewModel: FeedViewModel`, `@State private var current: SellUiState?`
	//
	// Arrays are deliberately excluded rather than unwrapped. `[FeedItem]` is a
	// Swift Array whose members are Swift's — `isEmpty`, `enumerated`, `sorted`
	// — and treating it as its element type reported every one of those as a
	// missing Kotlin member. That was most of the checker's first output and
	// all of it was noise.
	reSwiftDecl = regexp.MustCompile(
		`(?m)(?:let|var)\s+(\w+)\s*:\s*([A-Z]\w*)\??\s*(?:=|$|\{)`)

	// `receiver.member(` or `receiver.member`
	reSwiftUse = regexp.MustCompile(`\b(\w+)\.(\w+)\s*(\()?`)

	// A Swift type declaration, which is where a receiver's scope begins.
	//
	// Receivers were resolved per *file* at first, so `state` in SearchScreen
	// (a SearchUiState) and `state` in MeScreen (a MyListingsUiState) — same
	// file — were the same name bound twice, and whichever lost reported every
	// access as missing. Scoping to the enclosing type fixes it and is the
	// difference between a tool somebody reads and one they mute.
	reSwiftScope = regexp.MustCompile(`(?m)^\s*(?:private\s+|public\s+|internal\s+)?(?:struct|final class|class|enum|extension)\s+(\w+)`)

	// `extension Failure { var words: String ... }` — Swift-side members on a
	// Kotlin type. Real API from Swift's point of view, invisible to Kotlin.
	reSwiftExtension = regexp.MustCompile(`(?m)^\s*extension\s+([A-Z]\w*)\s*\{`)
	reSwiftMember    = regexp.MustCompile(`(?m)^\s*(?:private\s+|public\s+|static\s+)*(?:var|let|func)\s+(\w+)`)
)

type finding struct {
	file, detail string
	line         int
}

func scanSwift(dir string, kotlin map[string]*kotlinType) []finding {
	var findings []finding

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".swift") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		src := string(raw)

		// Swift extensions add members to Kotlin types, and from a call site
		// they are indistinguishable from Kotlin's own. Folded in before any
		// checking, so `failure.words` resolves.
		for _, m := range reSwiftExtension.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			t, ok := kotlin[name]
			if !ok {
				continue
			}
			// To the end of the file is over-broad and safe: it can only widen
			// what the checker accepts, never invent a finding.
			for _, mm := range reSwiftMember.FindAllStringSubmatch(src[m[1]:], -1) {
				t.members[mm[1]] = true
			}
		}

		// Receivers, scoped to the Swift type that declares them.
		lines := strings.Split(src, "\n")
		scope := ""
		receivers := map[string]map[string]string{} // scope → name → Kotlin type

		for _, line := range lines {
			if m := reSwiftScope.FindStringSubmatch(line); m != nil {
				scope = m[1]
			}
			if m := reSwiftDecl.FindStringSubmatch(line); m != nil {
				if _, ok := kotlin[m[2]]; ok {
					if receivers[scope] == nil {
						receivers[scope] = map[string]string{}
					}
					receivers[scope][m[1]] = m[2]
				}
			}
		}

		scope = ""
		for i, line := range lines {
			if m := reSwiftScope.FindStringSubmatch(line); m != nil {
				scope = m[1]
			}
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
				continue
			}
			bound := receivers[scope]
			if bound == nil {
				continue
			}
			for _, m := range reSwiftUse.FindAllStringSubmatch(line, -1) {
				typeName, ok := bound[m[1]]
				if !ok {
					continue
				}
				t := kotlin[typeName]
				member := m[2]
				// Kotlin/Native and Swift synthetics that never appear in the
				// Kotlin source.
				switch member {
				case "value", "shared", "companion", "doCopy", "description",
					"hash", "isEqual", "self", "map", "flatMap", "count":
					continue
				}
				if !t.members[member] {
					findings = append(findings, finding{
						file:   filepath.Base(path),
						line:   i + 1,
						detail: fmt.Sprintf("%s.%s — no such member on Kotlin %s", m[1], member, typeName),
					})
					continue
				}

				// Argument labels, when this is a call and the function's
				// parameters are known.
				//
				// The other half of the same failure class, and the one SKIE
				// makes easy to get wrong: Kotlin parameter names become Swift
				// argument labels verbatim, so renaming a parameter is a source
				// break in the Swift that Kotlin will never mention. A call
				// with the right name and the wrong labels compiles on the
				// Kotlin side forever.
				if m[3] != "(" {
					continue
				}
				want, known := t.funcs[member]
				if !known {
					continue
				}
				call := line[strings.Index(line, m[0])+len(m[0]):]
				got := argumentLabels(call)
				if got == nil {
					// Unbalanced on this line — a call wrapped across lines.
					// Skipped rather than guessed at.
					continue
				}
				if !labelsMatch(want, got) {
					findings = append(findings, finding{
						file: filepath.Base(path),
						line: i + 1,
						detail: fmt.Sprintf("%s.%s(%s) — Kotlin declares (%s)",
							m[1], member, strings.Join(got, ":"), strings.Join(want, ":")),
					})
				}
			}
		}
		return nil
	})
	return findings
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}

	kotlin := scanKotlin([]string{
		filepath.Join(root, "shared", "src", "commonMain"),
		filepath.Join(root, "shared", "src", "iosMain"),
	})
	if len(kotlin) == 0 {
		fmt.Fprintln(os.Stderr, "interopcheck: found no Kotlin types — wrong directory?")
		os.Exit(2)
	}

	findings := scanSwift(filepath.Join(root, "iosApp"), kotlin)

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].file != findings[j].file {
			return findings[i].file < findings[j].file
		}
		return findings[i].line < findings[j].line
	})

	fmt.Printf("interopcheck: %d Kotlin types, %d findings\n", len(kotlin), len(findings))
	for _, f := range findings {
		fmt.Printf("  %s:%d  %s\n", f.file, f.line, f.detail)
	}
	if len(findings) > 0 {
		os.Exit(1)
	}
}

// argumentLabels reads the labels out of a Swift call's argument list.
//
// Returns nil when the parentheses do not balance on the line, which means the
// call is wrapped and this line does not hold all of it. Skipping those is a
// deliberate loss of coverage: guessing at a partial argument list produces
// findings that are wrong, and one wrong finding costs more trust than ten
// missed ones buy.
func argumentLabels(s string) []string {
	depth := 1
	field := strings.Builder{}
	var labels []string

	flush := func() {
		a := strings.TrimSpace(field.String())
		field.Reset()
		if a == "" {
			return
		}
		// A trailing closure or an unlabelled argument has no colon before the
		// first delimiter; Kotlin exports every parameter with a label, so an
		// unlabelled one is recorded as "_" and will not match.
		colon := strings.Index(a, ":")
		if colon <= 0 {
			labels = append(labels, "_")
			return
		}
		label := strings.TrimSpace(a[:colon])
		if strings.ContainsAny(label, " .()[]\"") {
			labels = append(labels, "_")
			return
		}
		labels = append(labels, label)
	}

	inString := false
	for _, r := range s {
		if r == '"' {
			inString = !inString
		}
		if !inString {
			switch r {
			case '(', '[', '{':
				depth++
			case ')', ']', '}':
				depth--
				if depth == 0 {
					flush()
					return labels
				}
			case ',':
				if depth == 1 {
					flush()
					continue
				}
			}
		}
		field.WriteRune(r)
	}
	return nil // unbalanced: the call spans lines
}

// labelsMatch compares a Swift call's labels against Kotlin's parameter names.
//
// Trailing parameters may be omitted — Kotlin default arguments that SKIE did
// export — so a prefix match is accepted. An out-of-order or misspelled label
// is not: those are the mistakes worth reporting.
func labelsMatch(want, got []string) bool {
	if len(got) > len(want) {
		return false
	}
	for i, g := range got {
		if g == "_" {
			// A trailing closure or an unlabelled argument. Not something this
			// checker can adjudicate.
			continue
		}
		if g != want[i] {
			return false
		}
	}
	return true
}
