// Package skills scans Claude skill directories (global, project, plugin) and
// returns a categorized list suitable for display in the Skills dashboard tab.
package skills

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Process-wide TTL cache for the result of a full skills walk. The
// scan over 1000+ plugin SKILL.md files is the main reason the
// /api/v1/skills endpoint used to take 30-45 seconds — and the
// frontend fetches it twice (React Strict Mode), so without a cache
// the user pays for it twice in a row. 60-second TTL is short enough
// that a freshly-installed plugin shows up on the next refresh, long
// enough that tab navigation feels instant.
var (
	skillCacheMu      sync.Mutex
	skillCacheRows    []Skill
	skillCacheExpires time.Time
)

const skillCacheTTL = 60 * time.Second

// Source identifies where a skill lives.
type Source string

const (
	SourceGlobal  Source = "global"
	SourceProject Source = "project"
	SourcePlugin  Source = "plugin"
)

// Skill is the JSON-facing representation.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Source      Source `json:"source"`
	Plugin      string `json:"plugin,omitempty"`
	Path        string `json:"path"`
}

// MCPServer is a flattened view of one entry in `.mcp.json`.
type MCPServer struct {
	Name      string `json:"name"`
	Group     string `json:"group,omitempty"`
	Comment   string `json:"comment,omitempty"`
	Transport string `json:"transport"`
	URL       string `json:"url,omitempty"`
	Command   string `json:"command,omitempty"`
	Enabled   bool   `json:"enabled"`
	Source    Source `json:"source"`
}

// scanner walks the directory roots and parses SKILL.md frontmatter.
type scanner struct {
	homeDir    string
	projectDir string
}

// New returns a ready-to-use Scanner bound to the home dir and a project dir.
func New(projectDir string) *scanner {
	h, _ := os.UserHomeDir()
	return &scanner{homeDir: h, projectDir: projectDir}
}

// ListSkills walks the known skill roots in parallel and returns the union.
// Skills with identical names are de-duplicated; project wins over plugin
// wins over global.
//
// Result is cached for skillCacheTTL — a follow-up call within the
// window returns the previous result instantly. Call InvalidateCache()
// after creating / editing / deleting a skill to force a fresh walk.
func (s *scanner) ListSkills() []Skill {
	skillCacheMu.Lock()
	if skillCacheRows != nil && time.Now().Before(skillCacheExpires) {
		out := make([]Skill, len(skillCacheRows))
		copy(out, skillCacheRows)
		skillCacheMu.Unlock()
		return out
	}
	skillCacheMu.Unlock()

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		rows []Skill
	)

	collect := func(dir string, src Source, plugin string) {
		defer wg.Done()
		found, err := readSkillRoot(dir, src, plugin)
		if err != nil {
			return
		}
		mu.Lock()
		rows = append(rows, found...)
		mu.Unlock()
	}

	if s.homeDir != "" {
		wg.Add(1)
		go collect(filepath.Join(s.homeDir, ".claude", "skills"), SourceGlobal, "")

		// Plugin skills live under two possible layouts depending on how the
		// user's Claude Code plugins were installed. Scan both so nothing is
		// missed regardless of version.
		for _, base := range []string{"repos", "marketplaces"} {
			root := filepath.Join(s.homeDir, ".claude", "plugins", base)
			entries, err := os.ReadDir(root)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				wg.Add(1)
				go collect(
					filepath.Join(root, e.Name(), "skills"),
					SourcePlugin,
					e.Name(),
				)
			}
		}
	}

	if s.projectDir != "" {
		wg.Add(1)
		go collect(filepath.Join(s.projectDir, ".claude", "skills"), SourceProject, "")
		// Some projects capitalise the directory.
		wg.Add(1)
		go collect(filepath.Join(s.projectDir, ".claude", "Skills"), SourceProject, "")
	}

	wg.Wait()

	// De-dupe by name; priority: project > plugin > global.
	priority := map[Source]int{SourceProject: 3, SourcePlugin: 2, SourceGlobal: 1}
	byName := make(map[string]Skill, len(rows))
	for _, r := range rows {
		if cur, ok := byName[r.Name]; !ok || priority[r.Source] > priority[cur.Source] {
			byName[r.Name] = r
		}
	}

	out := make([]Skill, 0, len(byName))
	for _, v := range byName {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Name < out[j].Name
	})

	skillCacheMu.Lock()
	skillCacheRows = make([]Skill, len(out))
	copy(skillCacheRows, out)
	skillCacheExpires = time.Now().Add(skillCacheTTL)
	skillCacheMu.Unlock()

	return out
}

// InvalidateCache drops the cached skill list — call after creating,
// editing, or deleting a SKILL.md so the next /skills call sees the
// change immediately rather than after the TTL expires.
func InvalidateCache() {
	skillCacheMu.Lock()
	skillCacheRows = nil
	skillCacheExpires = time.Time{}
	skillCacheMu.Unlock()
}

// readSkillRoot walks a single skills root and returns parsed skills.
func readSkillRoot(root string, src Source, plugin string) ([]Skill, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		mdPath := filepath.Join(root, e.Name(), "SKILL.md")
		name, desc, ok := readFrontmatter(mdPath)
		if !ok {
			// Without a valid SKILL.md the directory isn't a real skill —
			// skip it rather than polluting the catalogue with ".git" etc.
			continue
		}
		out = append(out, Skill{
			Name:        name,
			Description: desc,
			Category:    categorise(name, desc),
			Source:      src,
			Plugin:      plugin,
			Path:        mdPath,
		})
	}
	return out, nil
}

// readFrontmatter pulls `name` and `description` from a SKILL.md YAML block.
// Minimal parser — not a full YAML engine, just enough for our consistent
// skill file layout.
func readFrontmatter(path string) (name, desc string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", false
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*64), 1024*256)

	inFrontmatter := false
	lineCount := 0
	for sc.Scan() {
		line := sc.Text()
		lineCount++
		if lineCount == 1 {
			if strings.TrimSpace(line) != "---" {
				return "", "", false
			}
			inFrontmatter = true
			continue
		}
		if !inFrontmatter {
			break
		}
		if strings.TrimSpace(line) == "---" {
			break
		}
		if n, v, ok := splitYAMLPair(line); ok {
			switch n {
			case "name":
				name = v
			case "description":
				desc = v
			}
		}
	}
	return name, desc, name != ""
}

func splitYAMLPair(line string) (string, string, bool) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", "", false
	}
	k := strings.TrimSpace(line[:idx])
	v := strings.TrimSpace(line[idx+1:])
	v = strings.Trim(v, `"'`)
	return k, v, k != ""
}

// --- Categorisation ------------------------------------------------------

// Rule: if any pattern in `matchers` matches either the skill name or
// description (case-insensitive), bucket it in `category`. Rules are evaluated
// in order; first match wins. Keeps the UI list scannable (~12 buckets).
type categoryRule struct {
	category string
	matchers []*regexp.Regexp
}

var categoryRules = buildRules()

func buildRules() []categoryRule {
	mk := func(cat string, patterns ...string) categoryRule {
		re := make([]*regexp.Regexp, 0, len(patterns))
		for _, p := range patterns {
			re = append(re, regexp.MustCompile("(?i)"+p))
		}
		return categoryRule{category: cat, matchers: re}
	}
	// Order matters: narrower categories first.
	return []categoryRule{
		mk("Security", `\bsecurity\b`, `\bpenetration\b`, `\bpentest\b`, `\bvuln\b`, `\bauth\b`, `\bowasp\b`, `\bsast\b`, `\bfuzz\b`, `\bcrypto\b`, `malware`, `exploit`, `reverse.?engineer`, `privilege`),
		mk("Testing", `\btdd\b`, `\btest\b`, `\bplaywright\b`, `e2e`, `coverage`, `qa\b`, `\bcypress\b`, `\bjest\b`, `\bvitest\b`, `bats`, `testing`),
		mk("Cloud (AWS/Azure/GCP)", `\baws\b`, `\bazure\b`, `\bgcp\b`, `\bcloud\b`, `cloudformation`, `lambda`, `s3`, `cosmos`, `eventhub`, `servicebus`, `keyvault`, `\bterraform\b`, `cdk`, `kubernetes`, `k8s`, `helm`, `istio`, `gcloud`),
		mk("DevOps & Infra", `devops`, `docker`, `ci.?cd`, `github.?actions`, `gitlab.?ci`, `deployment`, `pipeline`, `observability`, `monitoring`, `prometheus`, `grafana`, `incident`, `sre`, `slo`, `pagerduty`),
		mk("Frontend & UI", `frontend`, `\bui\b`, `\bux\b`, `react`, `next\.?js`, `vue`, `svelte`, `angular`, `tailwind`, `css`, `design`, `figma`, `canva`, `animation`, `webflow`, `shopify`),
		mk("Mobile", `mobile`, `flutter`, `swiftui`, `ios`, `android`, `kotlin.?coroutine`, `expo`, `jetpack`),
		mk("Backend & APIs", `backend`, `\bapi\b`, `fastapi`, `django`, `laravel`, `springboot`, `nestjs`, `graphql`, `rest`, `microservice`, `server.?management`, `dbos`, `cqrs`, `ddd`),
		mk("Data & DB", `\bsql\b`, `postgres`, `mysql`, `mongo`, `nosql`, `database`, `\bdbt\b`, `redshift`, `clickhouse`, `\bsupabase\b`, `airflow`, `data.?pipeline`, `data.?quality`),
		mk("AI & Agents", `\bllm\b`, `\bai\b`, `agent`, `rag\b`, `embedding`, `vector`, `prompt`, `claude`, `openai`, `gemini`, `anthropic`, `hugging.?face`, `langchain`, `langgraph`, `mcp.?builder`, `ml.?engineer`, `computer.?vision`, `voice`),
		mk("Languages", `typescript`, `javascript`, `python`, `\bgo(lang)?\b`, `\brust\b`, `\bc\+\+\b`, `\bjava\b`, `\bc#\b`, `csharp`, `kotlin`, `swift`, `ruby`, `scala`, `elixir`, `julia`, `perl`, `php`, `haskell`),
		mk("Integrations", `slack`, `discord`, `gmail`, `whatsapp`, `telegram`, `notion`, `airtable`, `linear`, `jira`, `zendesk`, `hubspot`, `stripe`, `clickup`, `trello`, `asana`, `monday`, `intercom`, `mailchimp`, `sendgrid`, `twilio`, `calendly`, `zoom`, `-automation\b`, `automate\s`, `activecampaign`, `amplitude`, `basecamp`, `bitbucket`, `brevo`, `cal\.com`, `close\b`, `coda`, `confluence`, `convertkit`, `docusign`, `dropbox`, `freshdesk`, `freshservice`, `intercom`, `klaviyo`, `mailchimp`, `miro`, `mixpanel`, `notion`, `posthog`, `reddit`, `render`, `salesforce`, `segment`, `sentry`, `shopify`, `square`, `supabase`, `todoist`, `twitter`, `webflow`, `wrike`, `youtube`, `zoho`),
		mk("Design & Docs", `documentation`, `doc.?generator`, `readme`, `changelog`, `wiki`, `tutorial`, `c4`, `adr`, `architecture.?decision`, `mermaid`, `docx`, `pdf`, `pptx`),
		mk("Workflow & Planning", `planning`, `plan.?writing`, `executing.?plans`, `roadmap`, `brainstorm`, `retrospective`, `debug`, `incident`, `onboarding`, `context-`, `memory`, `skill-creator`, `webhook`, `conductor-`, `\bclean.?code\b`, `refactor`, `\breview\b`, `\bcommit\b`, `pr-enhance`, `agent-evaluation`, `review-excellence`, `workflow`, `tech-debt`),
		mk("SEO & Marketing", `\bseo\b`, `marketing`, `paid.?ads`, `cro\b`, `launch`, `copywriting`, `content-`, `viral`),
		mk("Finance & Business", `\bbilling`, `pricing`, `stripe`, `paypal`, `finance`, `legal`, `hr\b`, `startup`, `business`, `sales`),
	}
}

func categorise(name, desc string) string {
	hay := strings.ToLower(name + " " + desc)
	for _, r := range categoryRules {
		for _, re := range r.matchers {
			if re.MatchString(hay) {
				return r.category
			}
		}
	}
	return "Other"
}
