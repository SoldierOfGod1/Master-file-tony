package darknoc

import (
	"bufio"
	"os"
	"strings"
)

// LoadRegistry parses the Capgemini Open Registry agent block from
// the supplied DarkNoc.md file. The doc has a repeating, free-form
// structure: a 1-letter domain glyph line, the domain, a category,
// the agent name, a summary paragraph, then "Protocol", a protocol
// name, "Use Case", and a use-case label. We walk it linearly.
//
// The parser is intentionally tolerant — if the doc shape changes,
// we don't want to break startup. On any parse hiccup we return what
// we managed to extract and the caller falls back to the empty list
// (the page tile shows "// registry not loaded" — Section 4 plan).
func LoadRegistry(path string) []RegistryAgent {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*64), 1024*512)

	// Read whole doc into a slice — parsing is stateful enough that a
	// streaming scanner with line-of-sight is harder than buffered
	// random access.
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}

	// The agent block starts after "Showing 41 of 41 agents". Bail
	// early if we don't find it — the doc may have been replaced.
	start := -1
	for i, l := range lines {
		if strings.Contains(l, "Showing 41 of 41 agents") {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return nil
	}

	out := make([]RegistryAgent, 0, 41)

	// Walk forward looking for the 9-line shape:
	//   <domain glyph (1 char)>
	//   <Domain>
	//   <Category>
	//   <Name>
	//   "" (blank)
	//   <Summary>
	//   "" (blank)
	//   "Protocol"
	//   <protocol>
	//   "" (blank)
	//   "Use Case"
	//   <use-case>
	//
	// In practice the doc is messier — blank lines drift and some
	// blocks are missing the trailing use-case. So we look for
	// "Protocol" / "Use Case" anchor lines and pull the surrounding
	// fields by relative offset.
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "Protocol" {
			continue
		}
		// Walk backwards to find the preceding non-empty lines as
		// (glyph, domain, category, name, summary).
		fields := []string{}
		j := i - 1
		for j >= 0 && len(fields) < 5 {
			t := strings.TrimSpace(lines[j])
			if t != "" {
				fields = append(fields, t)
			}
			j--
		}
		if len(fields) < 5 {
			continue
		}
		// fields is reversed: [summary, name, category, domain, glyph]
		summary := fields[0]
		name := fields[1]
		category := fields[2]
		domain := fields[3]

		// Protocol is on the next non-empty line after "Protocol".
		protocol := nextNonEmpty(lines, i+1)

		// Use Case anchor is a few lines further on.
		useCase := ""
		for k := i + 1; k < len(lines) && k < i+10; k++ {
			if strings.TrimSpace(lines[k]) == "Use Case" {
				useCase = nextNonEmpty(lines, k+1)
				break
			}
		}

		out = append(out, RegistryAgent{
			Name:     name,
			Domain:   domain,
			Category: category,
			Summary:  summary,
			Protocol: protocol,
			UseCase:  useCase,
		})
		if len(out) >= 41 {
			break
		}
	}

	return out
}

func nextNonEmpty(lines []string, from int) string {
	for k := from; k < len(lines); k++ {
		t := strings.TrimSpace(lines[k])
		if t != "" {
			return t
		}
	}
	return ""
}

// DefaultRegistryPath is the fallback location for the source doc
// when no explicit path is configured. The user's Downloads folder
// matches where the doc lives today; ~/.claude/dark-noc-registry.md
// is the long-term home (one-line copy operation, deferred).
func DefaultRegistryPath() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	return home + string(os.PathSeparator) + "Downloads" + string(os.PathSeparator) + "DarkNoc.md"
}
