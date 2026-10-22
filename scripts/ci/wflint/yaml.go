package main

import (
	"bytes"
	"errors"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// parse decodes one YAML document and runs the checks that hold for every file
// kind: anchors and aliases, explicit tags, duplicate keys and secrets.
func (l *linter) parse(rel string, data []byte) *yaml.Node {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		l.add(rel, 0, "yaml", "does not parse: %v", err)
		return nil
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		l.add(rel, extra.Line, "yaml", "more than one YAML document")
	}
	l.scanRawSecrets(rel, string(data))
	l.walk(rel, &doc, "")
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		l.add(rel, doc.Line, "structure", "the top level must be a mapping")
		return nil
	}
	return doc.Content[0]
}

var (
	exprRe      = regexp.MustCompile(`(?s)\$\{\{(.*?)\}\}`)
	secretsWord = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_-])secrets([^A-Za-z0-9_-]|$)`)
	ghTokenRe   = regexp.MustCompile(`(?i)github\s*\.\s*token|github\s*\[\s*['"]token['"]\s*\]`)
)

func exprLeaksSecret(expr string) string {
	switch {
	case secretsWord.MatchString(expr):
		return "secrets"
	case ghTokenRe.MatchString(expr):
		return "github.token"
	}
	return ""
}

// scanRawSecrets looks at the file as text, so expressions in comments, in run
// blocks and inside any string are caught before YAML decoding drops comments.
func (l *linter) scanRawSecrets(rel, text string) {
	for _, m := range exprRe.FindAllStringSubmatchIndex(text, -1) {
		if what := exprLeaksSecret(text[m[2]:m[3]]); what != "" {
			l.add(rel, 1+strings.Count(text[:m[0]], "\n"), "secrets", "%s in ${{ }}: no secrets before Wave 5 (SC-PL-04)", what)
		}
	}
}

// walk visits every node: anchors, aliases, tags, duplicate keys, decoded scalars.
func (l *linter) walk(rel string, n *yaml.Node, key string) {
	if n == nil {
		return
	}
	if n.Anchor != "" {
		l.add(rel, n.Line, "anchor", "YAML anchor &%s: anchors and aliases are not allowed", n.Anchor)
	}
	if n.Kind == yaml.AliasNode {
		l.add(rel, n.Line, "anchor", "YAML alias *%s: anchors and aliases are not allowed", n.Value)
		return
	}
	if n.Style&yaml.TaggedStyle != 0 {
		l.add(rel, n.Line, "yaml", "explicit tag %s is not allowed", n.Tag)
	}
	switch n.Kind {
	case yaml.ScalarNode:
		// Decoded value: catches escapes such as "\x24{{ secrets.X }}".
		for _, m := range exprRe.FindAllStringSubmatch(n.Value, -1) {
			if what := exprLeaksSecret(m[1]); what != "" {
				l.add(rel, n.Line, "secrets", "%s in ${{ }}: no secrets before Wave 5 (SC-PL-04)", what)
			}
		}
		if key == "if" {
			if what := exprLeaksSecret(n.Value); what != "" {
				l.add(rel, n.Line, "secrets", "%s in an if: condition", what)
			}
		}
	case yaml.MappingNode:
		keys := map[string]bool{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			l.walk(rel, k, "")
			if k.Kind == yaml.ScalarNode {
				if keys[k.Value] {
					l.add(rel, k.Line, "yaml", "duplicate key %q", k.Value)
				}
				keys[k.Value] = true
				if strings.EqualFold(k.Value, "secrets") {
					l.add(rel, k.Line, "secrets", "a secrets: key (secrets: inherit or a secrets map) passes secrets on")
				}
				switch strings.ToUpper(k.Value) {
				case "MAKEFLAGS", "GNUMAKEFLAGS", "MFLAGS", "MAKEFILES", "MAKEOVERRIDES":
					l.add(rel, k.Line, "make", "%s changes how make runs; not allowed", k.Value)
				}
			} else {
				l.add(rel, k.Line, "yaml", "a mapping key must be a plain scalar")
			}
			l.walk(rel, v, scalarKey(k))
		}
		l.checkMapping(rel, n)
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, c := range n.Content {
			l.walk(rel, c, "")
		}
	}
}

func scalarKey(k *yaml.Node) string {
	if k.Kind == yaml.ScalarNode {
		return k.Value
	}
	return ""
}

// get returns the value node for key in mapping m (nil when absent).
func get(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Kind == yaml.ScalarNode && m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func getKey(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Kind == yaml.ScalarNode && m.Content[i].Value == key {
			return m.Content[i]
		}
	}
	return nil
}

func scalar(n *yaml.Node) (string, bool) {
	if n == nil || n.Kind != yaml.ScalarNode {
		return "", false
	}
	return n.Value, true
}

var (
	shaRe       = regexp.MustCompile(`^([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)(/[A-Za-z0-9_./-]+)?@([0-9a-f]{40})$`)
	versionCmt  = regexp.MustCompile(`^#\s*v[0-9]+\.[0-9]+\.[0-9]+\S*\s*$`)
	digestRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9._/:-]*@sha256:[0-9a-f]{64}$`)
	zeroSHA     = strings.Repeat("0", 40)
	githubLocal = ".github/actions/"
)

// checkMapping applies the keyed rules to any mapping, wherever it sits, so a
// step inside a composite action is held to the same rules as one in a workflow.
func (l *linter) checkMapping(rel string, m *yaml.Node) {
	if u := get(m, "uses"); u != nil {
		l.checkUses(rel, u)
		if v, ok := scalar(u); ok && strings.HasPrefix(v, "actions/checkout@") {
			pc, _ := scalar(get(get(m, "with"), "persist-credentials"))
			if pc != "false" {
				l.add(rel, u.Line, "checkout", "actions/checkout must set with: persist-credentials: false")
			}
		}
	}
	if s := get(m, "shell"); s != nil {
		if v, _ := scalar(s); v != "bash" {
			l.add(rel, s.Line, "shell", "shell must be bash (got %q)", v)
		}
	}
	if r := get(m, "run"); r != nil && r.Kind == yaml.ScalarNode {
		l.checkMakeIn(rel, r.Line, r.Value, 0)
	}
	if img := get(m, "image"); img != nil {
		l.checkImage(rel, img)
	}
	if c := get(m, "container"); c != nil && c.Kind == yaml.ScalarNode {
		l.checkImage(rel, c)
	}
	if s := get(m, "services"); s != nil && s.Kind == yaml.MappingNode {
		for i := 1; i < len(s.Content); i += 2 {
			if s.Content[i].Kind == yaml.ScalarNode {
				l.checkImage(rel, s.Content[i])
			}
		}
	}
}

func (l *linter) checkUses(rel string, u *yaml.Node) {
	v, ok := scalar(u)
	if !ok {
		l.add(rel, u.Line, "pin", "uses: must be a plain string")
		return
	}
	switch {
	case strings.HasPrefix(v, "./"):
		clean := path.Clean(strings.TrimPrefix(v, "./"))
		if !strings.HasPrefix(clean+"/", githubLocal) || clean+"/" == githubLocal || strings.Contains(v, "..") {
			l.add(rel, u.Line, "uses", "local uses: %s is outside .github/actions/", v)
		}
	case strings.HasPrefix(v, "docker://"):
		if !digestRe.MatchString(strings.TrimPrefix(v, "docker://")) {
			l.add(rel, u.Line, "pin", "uses: %s must pin the image by @sha256: digest", v)
		}
	default:
		m := shaRe.FindStringSubmatch(v)
		if m == nil {
			l.add(rel, u.Line, "pin", "uses: %s must be pinned by a full 40-hex commit SHA", v)
			return
		}
		if m[4] == zeroSHA {
			l.add(rel, u.Line, "pin", "uses: %s is pinned to the all-zero SHA", v)
		}
		if !versionCmt.MatchString(strings.TrimSpace(u.LineComment)) {
			l.add(rel, u.Line, "pin", "uses: %s needs a `# vX.Y.Z` comment on the same line", v)
		}
		if !l.owners[m[1]] {
			l.add(rel, u.Line, "uses", "action owner %q is not in scripts/ci/wflint/allowed-owners.txt", m[1])
		}
	}
}

func (l *linter) checkImage(rel string, n *yaml.Node) {
	v, ok := scalar(n)
	if !ok {
		l.add(rel, n.Line, "pin", "image must be a plain string")
		return
	}
	v = strings.TrimPrefix(v, "docker://")
	if !digestRe.MatchString(v) {
		l.add(rel, n.Line, "pin", "image %s must be pinned by @sha256: digest", v)
	}
}

var allowedEvents = map[string]bool{"pull_request": true, "push": true, "workflow_dispatch": true, "schedule": true}

var topKeys = map[string]bool{"name": true, "run-name": true, "on": true, "permissions": true, "env": true,
	"defaults": true, "concurrency": true, "jobs": true}

func (l *linter) lintWorkflow(rel string, data []byte) {
	top := l.parse(rel, data)
	if top == nil {
		return
	}
	for i := 0; i+1 < len(top.Content); i += 2 {
		k := top.Content[i]
		if !topKeys[k.Value] {
			l.add(rel, k.Line, "structure", "unknown top-level key %q", k.Value)
		}
	}

	// Triggers: an allow-list, compared after YAML decoding (escapes resolved).
	on := get(top, "on")
	switch {
	case on == nil:
		l.add(rel, top.Line, "trigger", "no on: key")
	case on.Kind == yaml.ScalarNode:
		l.checkEvent(rel, on)
	case on.Kind == yaml.SequenceNode:
		for _, e := range on.Content {
			l.checkEvent(rel, e)
		}
	case on.Kind == yaml.MappingNode:
		for i := 0; i < len(on.Content); i += 2 {
			l.checkEvent(rel, on.Content[i])
		}
	default:
		l.add(rel, on.Line, "trigger", "on: must be an event, a list or a mapping")
	}

	l.checkPermissions(rel, top, "workflow", false)

	// defaults: run: shell: bash, so every run step gets -eo pipefail.
	if sh, _ := scalar(get(get(get(top, "defaults"), "run"), "shell")); sh != "bash" {
		l.add(rel, top.Line, "shell", "the workflow must set defaults: run: shell: bash")
	}

	// concurrency group including the event name (a push cannot cancel a schedule).
	conc := get(top, "concurrency")
	group, _ := scalar(conc)
	if conc != nil && conc.Kind == yaml.MappingNode {
		group, _ = scalar(get(conc, "group"))
	}
	if conc == nil {
		l.add(rel, top.Line, "concurrency", "no concurrency group")
	} else if !regexp.MustCompile(`\$\{\{[^}]*github\.event_name[^}]*\}\}`).MatchString(group) {
		l.add(rel, conc.Line, "concurrency", "the concurrency group must include ${{ github.event_name }}")
	}

	jobs := get(top, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode || len(jobs.Content) == 0 {
		l.add(rel, top.Line, "structure", "jobs: must be a non-empty mapping")
		return
	}
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		name, job := jobs.Content[i].Value, jobs.Content[i+1]
		if job.Kind != yaml.MappingNode {
			l.add(rel, job.Line, "structure", "job %s must be a mapping", name)
			continue
		}
		l.checkPermissions(rel, job, "job "+name, l.idToken[rel])
		call := get(job, "uses") != nil
		to := get(job, "timeout-minutes")
		if call {
			if to != nil {
				l.add(rel, to.Line, "timeout", "job %s calls a reusable workflow; GitHub forbids timeout-minutes there", name)
			}
			continue
		}
		if to == nil {
			l.add(rel, job.Line, "timeout", "job %s has no timeout-minutes", name)
		} else if v, _ := scalar(to); !validTimeout(v) {
			l.add(rel, to.Line, "timeout", "job %s timeout-minutes must be a whole number from 1 to 360", name)
		}
		ro := get(job, "runs-on")
		if v, ok := scalar(ro); !ok || !regexp.MustCompile(`^ubuntu-[0-9]+\.[0-9]+$`).MatchString(v) {
			line := job.Line
			if ro != nil {
				line = ro.Line
			}
			l.add(rel, line, "runner", "job %s runs-on must be one GitHub-hosted ubuntu-NN.NN label", name)
		}
		if steps := get(job, "steps"); steps != nil {
			if steps.Kind != yaml.SequenceNode {
				l.add(rel, steps.Line, "structure", "job %s steps must be a list", name)
			} else {
				for _, s := range steps.Content {
					if s.Kind != yaml.MappingNode {
						l.add(rel, s.Line, "structure", "job %s has a step that is not a mapping", name)
					}
				}
			}
		}
	}
}

func validTimeout(v string) bool {
	n, err := strconv.Atoi(v)
	return err == nil && n >= 1 && n <= 360
}

func (l *linter) checkEvent(rel string, e *yaml.Node) {
	v, ok := scalar(e)
	if !ok || !allowedEvents[v] {
		l.add(rel, e.Line, "trigger", "event %q is not allowed (pull_request, push, workflow_dispatch, schedule only)", v)
	}
}

// checkPermissions: a mapping whose every value is read or none. idToken lets a
// job (never the workflow level) hold id-token: write; see id-token-allow.txt.
func (l *linter) checkPermissions(rel string, m *yaml.Node, where string, idToken bool) {
	p := get(m, "permissions")
	if p == nil {
		l.add(rel, m.Line, "permissions", "%s has no permissions: block", where)
		return
	}
	if p.Kind != yaml.MappingNode {
		v, _ := scalar(p)
		l.add(rel, p.Line, "permissions", "%s permissions must be a mapping of scopes to read or none (got %q)", where, v)
		return
	}
	for i := 0; i+1 < len(p.Content); i += 2 {
		k, v := p.Content[i], p.Content[i+1]
		val, ok := scalar(v)
		if ok && (val == "read" || val == "none") {
			continue
		}
		if ok && val == "write" && k.Value == "id-token" && idToken && where != "workflow" {
			continue
		}
		l.add(rel, v.Line, "permissions", "%s %s: %q; only read or none", where, k.Value, val)
	}
}

func (l *linter) lintAction(rel string, data []byte) {
	top := l.parse(rel, data)
	if top == nil {
		return
	}
	runs := get(top, "runs")
	using, _ := scalar(get(runs, "using"))
	if using != "composite" {
		l.add(rel, top.Line, "structure", "only composite actions are allowed (runs.using: %q)", using)
		return
	}
	steps := get(runs, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		l.add(rel, top.Line, "structure", "runs.steps must be a list")
		return
	}
	for _, s := range steps.Content {
		if s.Kind != yaml.MappingNode {
			l.add(rel, s.Line, "structure", "a step is not a mapping")
			continue
		}
		if get(s, "run") != nil && get(s, "shell") == nil {
			l.add(rel, s.Line, "shell", "a composite run step must set shell: bash")
		}
	}
}

// dependabot: every ecosystem the repo uses, weekly, with a 14-day cooldown (D-88).
var requiredEcosystems = []string{"github-actions", "docker", "docker-compose", "gomod", "npm"}

func (l *linter) lintDependabot(rel string, data []byte) {
	top := l.parse(rel, data)
	if top == nil {
		return
	}
	if v, _ := scalar(get(top, "version")); v != "2" {
		l.add(rel, top.Line, "dependabot", "version must be 2")
	}
	ups := get(top, "updates")
	if ups == nil || ups.Kind != yaml.SequenceNode {
		l.add(rel, top.Line, "dependabot", "updates must be a list")
		return
	}
	eco := map[string]bool{}
	gomodDirs := map[string]bool{}
	for _, u := range ups.Content {
		e, _ := scalar(get(u, "package-ecosystem"))
		eco[e] = true
		if iv, _ := scalar(get(get(u, "schedule"), "interval")); iv != "weekly" {
			l.add(rel, u.Line, "dependabot", "%s: schedule.interval must be weekly", e)
		}
		days, _ := scalar(get(get(u, "cooldown"), "default-days"))
		if n, err := strconv.Atoi(days); err != nil || n < 14 {
			l.add(rel, u.Line, "dependabot", "%s: cooldown.default-days must be at least 14 (D-88)", e)
		}
		dirs := []string{}
		if d, ok := scalar(get(u, "directory")); ok {
			dirs = append(dirs, d)
		}
		if ds := get(u, "directories"); ds != nil && ds.Kind == yaml.SequenceNode {
			for _, d := range ds.Content {
				if v, ok := scalar(d); ok {
					dirs = append(dirs, v)
				}
			}
		}
		if len(dirs) == 0 {
			l.add(rel, u.Line, "dependabot", "%s: no directory or directories", e)
		}
		if e == "gomod" {
			for _, d := range dirs {
				gomodDirs[strings.TrimSuffix(d, "/")] = true
			}
		}
	}
	for _, e := range requiredEcosystems {
		if !eco[e] {
			l.add(rel, top.Line, "dependabot", "no %s entry", e)
		}
	}
	for _, m := range l.goMods {
		if !gomodDirs[m] {
			l.add(rel, top.Line, "dependabot", "Go module %s is not in a gomod entry", m)
		}
	}
}
