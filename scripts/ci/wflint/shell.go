package main

import (
	"path"
	"regexp"
	"strings"
)

// A small shell lexer: enough to find command words and their arguments through
// quoting, line continuations, separators and comments. It is deliberately
// conservative: text it cannot place is reported, not skipped.

type token struct {
	s      string // the word with quotes removed and escapes resolved
	quoted bool   // some part of the word was quoted
	sep    bool   // a command boundary (; & | && || newline ( ) ` and keywords)
	redir  bool   // a redirection operator
	kw     string // for a keyword separator: the keyword (in, do, then, ...)
	line   int
}

var keywordSeps = map[string]bool{"{": true, "}": true, "then": true, "do": true, "else": true, "elif": true,
	"if": true, "while": true, "until": true, "!": true, "fi": true, "done": true, "esac": true, "in": true}

var simpleVarParen = regexp.MustCompile(`^\$\([A-Za-z_][A-Za-z0-9_]*\)`)

func lex(src string, line int) []token {
	var out []token
	var cur strings.Builder
	inWord, quoted := false, false
	wordLine := line
	flush := func() {
		if !inWord {
			return
		}
		w := cur.String()
		if !quoted && keywordSeps[w] {
			out = append(out, token{sep: true, kw: w, line: wordLine})
		} else {
			out = append(out, token{s: w, quoted: quoted, line: wordLine})
		}
		cur.Reset()
		inWord, quoted = false, false
	}
	start := func() {
		if !inWord {
			inWord, wordLine = true, line
		}
	}
	sep := func() { flush(); out = append(out, token{sep: true, line: line}) }

	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case c == '\\':
			if i+1 < len(src) && src[i+1] == '\n' {
				i++
				line++
				continue
			}
			start()
			if i+1 < len(src) {
				i++
				cur.WriteByte(src[i])
			}
		case c == '\n':
			sep()
			line++
		case c == ' ' || c == '\t' || c == '\r':
			flush()
		case c == '#' && !inWord:
			for i+1 < len(src) && src[i+1] != '\n' {
				i++
			}
		case c == '\'':
			start()
			quoted = true
			for i+1 < len(src) && src[i+1] != '\'' {
				i++
				if src[i] == '\n' {
					line++
				}
				cur.WriteByte(src[i])
			}
			i++
		case c == '"':
			start()
			quoted = true
			for i+1 < len(src) && src[i+1] != '"' {
				i++
				switch {
				case src[i] == '\\' && i+1 < len(src) && src[i+1] == '\n':
					i++
					line++
				case src[i] == '\\' && i+1 < len(src) && strings.IndexByte("\"\\$`", src[i+1]) >= 0:
					i++
					cur.WriteByte(src[i])
				default:
					if src[i] == '\n' {
						line++
					}
					cur.WriteByte(src[i])
				}
			}
			i++
		case c == '`':
			sep()
		case c == ';' || c == '&' || c == '|':
			if c == '&' && i+1 < len(src) && src[i+1] == '>' { // &> file
				flush()
				i++
				for i+1 < len(src) && src[i+1] == '>' {
					i++
				}
				out = append(out, token{redir: true, line: line})
				continue
			}
			sep()
			if i+1 < len(src) && (src[i+1] == c || (c == '|' && src[i+1] == '&')) {
				i++
			}
		case c == '$' && i+1 < len(src) && src[i+1] == '{':
			start()
			depth := 0
			for ; i < len(src); i++ {
				cur.WriteByte(src[i])
				if src[i] == '{' {
					depth++
				} else if src[i] == '}' {
					depth--
					if depth == 0 {
						break
					}
				} else if src[i] == '\n' {
					line++
				}
			}
		case c == '$' && i+1 < len(src) && src[i+1] == '(':
			if m := simpleVarParen.FindString(src[i:]); m != "" { // make-style $(NAME)
				start()
				cur.WriteString(m)
				i += len(m) - 1
				continue
			}
			sep()
			i++
		case c == '(' || c == ')':
			sep()
		case c == '<' || c == '>':
			if inWord && !quoted && isDigits(cur.String()) {
				cur.Reset()
				inWord = false
			}
			flush()
			op := string(c)
			for i+1 < len(src) && strings.IndexByte("<>&|-", src[i+1]) >= 0 {
				i++
				op += string(src[i])
			}
			t := token{redir: true, line: line, s: op}
			if strings.HasSuffix(op, "&") && i+1 < len(src) && (src[i+1] == '-' || (src[i+1] >= '0' && src[i+1] <= '9')) {
				i++ // >&2: the target is part of the operator
				t.s += string(src[i])
			}
			out = append(out, t)
		default:
			start()
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// commands splits a token list at separators.
// wordLists returns, per command from commands(), whether it is the word list of a
// `for NAME in ...` or `select NAME in ...` (data, not a command), and the loop
// variable it feeds. `for t in terraform; do $t apply; done` makes t an alias.
func wordLists(toks []token) []string {
	var out []string
	var cur []token
	prev, prevKw := []token(nil), ""
	flush := func(kw string) {
		if len(cur) > 0 {
			loopVar := ""
			if prevKw == "in" && len(prev) == 2 && !prev[0].quoted && (prev[0].s == "for" || prev[0].s == "select") {
				loopVar = prev[1].s
			}
			out = append(out, loopVar)
			prev = cur
		} else if kw != "in" {
			prev = nil
		}
		prevKw = kw
		cur = nil
	}
	for _, t := range toks {
		if t.sep {
			flush(t.kw)
			continue
		}
		cur = append(cur, t)
	}
	flush("")
	return out
}

func commands(toks []token) [][]token {
	var out [][]token
	var cur []token
	for _, t := range toks {
		if t.sep {
			if len(cur) > 0 {
				out = append(out, cur)
			}
			cur = nil
			continue
		}
		cur = append(cur, t)
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// redirTakesTarget: `> file` has a separate target word; `>&2` carries it.
func redirTakesTarget(t token) bool {
	return !(strings.Contains(t.s, "&") && len(t.s) > 1 && !strings.HasSuffix(t.s, "&"))
}

var (
	assignRe   = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)(\[[^]]*\])?\+?=(.*)$`)
	varRefRe   = regexp.MustCompile(`^\$(?:\{([A-Za-z_][A-Za-z0-9_]*)\}|\(([A-Za-z_][A-Za-z0-9_]*)\)|([A-Za-z_][A-Za-z0-9_]*))$`)
	makeWordRe = regexp.MustCompile(`(^|[^A-Za-z0-9_-])g?make($|[^A-Za-z0-9_-])`)
	makeFlagRe = regexp.MustCompile(`(?i)^(MAKEFLAGS|GNUMAKEFLAGS|MFLAGS|MAKEFILES|MAKEOVERRIDES)$`)
)

func varName(s string) string {
	m := varRefRe.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return m[1] + m[2] + m[3]
}

func isMakeBinary(s string) bool {
	b := path.Base(s)
	return (b == "make" || b == "gmake") && (s == b || strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./"))
}

func isShell(s string) bool {
	switch path.Base(s) {
	case "bash", "sh", "zsh", "dash", "ksh":
		return true
	}
	return false
}

// checkMakeIn checks every make call in a run block (and in bash -c / sh -c / eval
// strings inside it): targets must be canonical and literal.
func (l *linter) checkMakeIn(rel string, line int, script string, depth int) {
	if depth > 3 {
		l.add(rel, line, "make", "shell nesting too deep to check")
		return
	}
	toks := lex(script, line)
	aliases := map[string]bool{}
	for _, t := range toks {
		if m := assignRe.FindStringSubmatch(t.s); m != nil && !t.sep {
			if isMakeBinary(m[3]) || aliases[varName(m[3])] {
				aliases[m[1]] = true
			}
			if makeFlagRe.MatchString(m[1]) {
				l.add(rel, t.line, "make", "%s changes how make runs; not allowed", m[1])
			}
		}
	}
	for _, cmd := range commands(toks) {
		for i := 0; i < len(cmd); i++ {
			t := cmd[i]
			if t.redir {
				continue
			}
			if isMakeBinary(t.s) || aliases[varName(t.s)] {
				for j := i - 1; j >= 0 && assignRe.MatchString(cmd[j].s); j-- {
					l.add(rel, cmd[j].line, "make", "environment assignment %q before make", strings.SplitN(cmd[j].s, "=", 2)[0]+"=")
				}
				l.checkMakeArgs(rel, cmd[i+1:])
				break
			}
			if isShell(t.s) || t.s == "eval" {
				rest := cmd[i+1:]
				if t.s == "eval" {
					var parts []string
					for _, r := range rest {
						parts = append(parts, r.s)
					}
					l.checkMakeIn(rel, t.line, strings.Join(parts, " "), depth+1)
					break
				}
				for j := 0; j < len(rest); j++ {
					o := rest[j].s
					if strings.HasPrefix(o, "-") && !strings.HasPrefix(o, "--") && strings.Contains(o, "c") {
						if j+1 < len(rest) {
							l.checkMakeIn(rel, rest[j+1].line, rest[j+1].s, depth+1)
						}
						break
					}
					if !strings.HasPrefix(o, "-") {
						break
					}
				}
				// The words after a shell are its script or options; anything that
				// still mentions make is reported below unless it was the -c script.
				i = len(cmd)
				break
			}
			if m := assignRe.FindStringSubmatch(t.s); m != nil && aliases[m[1]] {
				continue // m=make
			}
			if makeWordRe.MatchString(t.s) {
				l.add(rel, t.line, "make", "make appears in %q, where its targets cannot be checked", t.s)
			}
		}
	}
}

var allowedMakeOpts = map[string]bool{"-s": true, "--silent": true, "--quiet": true, "-k": true, "--keep-going": true,
	"-n": true, "--dry-run": true, "--no-print-directory": true, "-j": true, "--jobs": true}

var jobsOptRe = regexp.MustCompile(`^(-j[0-9]+|--jobs=[0-9]+)$`)

func (l *linter) checkMakeArgs(rel string, args []token) {
	targets, before := 0, len(l.findings)
	line := 0
	for j := 0; j < len(args); j++ {
		a := args[j]
		line = a.line
		if a.redir {
			if redirTakesTarget(a) {
				j++
			}
			continue
		}
		s := a.s
		switch {
		case strings.Contains(s, "${{"):
			l.add(rel, a.line, "make", "make argument %q uses a ${{ }} expression", s)
		case s == "-C" || s == "--directory":
			if j+1 >= len(args) || args[j+1].s != "." {
				l.add(rel, a.line, "make", "make -C is only allowed with . (the repo root)")
			}
			j++
		case strings.HasPrefix(s, "--directory="), strings.HasPrefix(s, "-C"):
			v := strings.TrimPrefix(strings.TrimPrefix(s, "--directory="), "-C")
			if v != "." {
				l.add(rel, a.line, "make", "make -C is only allowed with . (the repo root)")
			}
		case (s == "-j" || s == "--jobs") && j+1 < len(args) && isDigits(args[j+1].s):
			j++
		case jobsOptRe.MatchString(s), allowedMakeOpts[s]:
		case strings.HasPrefix(s, "-"):
			l.add(rel, a.line, "make", "make option %q is not allowed (only -s -k -n -j and -C .)", s)
		case assignRe.MatchString(s) || strings.Contains(s, ":="):
			l.add(rel, a.line, "make", "make variable override %q is not allowed", s)
		case strings.ContainsAny(s, "$`*?["):
			l.add(rel, a.line, "make", "make target %q is not a literal", s)
		case !l.canonical[s]:
			l.add(rel, a.line, "make", "make %s: only canonical targets (D-56)", s)
		default:
			targets++
		}
	}
	if targets == 0 && len(l.findings) == before {
		l.add(rel, line, "make", "make without a target")
	}
}

// ---- Terraform ----------------------------------------------------------------

type tfMode int

const (
	tfNone tfMode = iota
	tfBinary
	tfImage
)

type tfFinding struct {
	line int
	msg  string
}

type tfScanner struct {
	vars  map[string]tfMode // tf=terraform, TERRAFORM_IMAGE=hashicorp/terraform:...
	funcs map[string]bool   // shell functions that pass "$@" to terraform
	files map[string]string
}

func newTFScanner() *tfScanner {
	return &tfScanner{vars: map[string]tfMode{"TERRAFORM_IMAGE": tfImage}, funcs: map[string]bool{}, files: map[string]string{}}
}

// Terraform's own subcommands that stay read-only and offline (D-16).
var tfAllowed = map[string]bool{"fmt": true, "validate": true, "version": true, "init": true, "providers": true}

const tfPassthroughFile = "scripts/ci/terraform.sh"

func isTFBinary(s string) bool {
	if s == "terraform" {
		return true
	}
	return path.Base(s) == "terraform" && (strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") || strings.Contains(s, "bin/"))
}

func (t *tfScanner) modeOf(s string) tfMode {
	switch {
	case isTFBinary(s):
		return tfBinary
	case strings.Contains(s, "hashicorp/terraform"):
		return tfImage
	case t.funcs[s]:
		return tfBinary
	}
	if v := varName(s); v != "" {
		return t.vars[v]
	}
	return tfNone
}

var makeAssignOps = map[string]bool{"=": true, ":=": true, "::=": true, "?=": true, "+=": true, "!=": true}
var makeAssignRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*[:?+!]?:?=(.*)$`)

// collect records aliases from one file; finish resolves chains (a=terraform; b=$a).
func (t *tfScanner) collect(rel, text string) { t.files[rel] = text }

func (t *tfScanner) finish() {
	for round := 0; round < 5; round++ {
		changed := false
		for _, text := range t.files {
			toks := lex(text, 1)
			for i, tk := range toks {
				name, val := "", ""
				if m := makeAssignRe.FindStringSubmatch(tk.s); m != nil && !tk.sep {
					name, val = m[1], m[2]
				} else if i+2 < len(toks) && makeAssignOps[toks[i+1].s] && regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(tk.s) {
					name, val = tk.s, toks[i+2].s
				}
				if name == "" {
					continue
				}
				if m := t.modeOf(strings.TrimSpace(val)); m != tfNone && t.vars[name] != m {
					t.vars[name] = m
					changed = true
				}
			}
		}
		// Functions whose body passes "$@" to terraform become aliases too.
		for _, text := range t.files {
			for _, fn := range t.passthroughFuncs(text) {
				if !t.funcs[fn] {
					t.funcs[fn] = true
					changed = true
				}
			}
		}
		if !changed {
			return
		}
	}
}

var funcStart = regexp.MustCompile(`^\s*(?:function\s+)?([A-Za-z_][A-Za-z0-9_-]*)\s*\(\)\s*\{?`)

// funcAt maps line numbers to the shell function they sit in.
func funcAt(text string) map[int]string {
	out := map[int]string{}
	cur := ""
	for i, ln := range strings.Split(text, "\n") {
		if m := funcStart.FindStringSubmatch(ln); m != nil {
			cur = m[1]
		}
		if cur != "" {
			out[i+1] = cur
		}
		if strings.HasPrefix(ln, "}") || (cur != "" && funcStart.MatchString(ln) && strings.HasSuffix(strings.TrimSpace(ln), "}")) {
			cur = ""
		}
	}
	return out
}

func isPassthrough(s string) bool {
	return s == "$@" || s == "$*" || s == "${@}" || s == "${*}"
}

func (t *tfScanner) passthroughFuncs(text string) []string {
	fa := funcAt(text)
	var out []string
	for _, cmd := range commands(lex(text, 1)) {
		for i, tk := range cmd {
			if mode := t.modeOf(tk.s); mode != tfNone && !assignRe.MatchString(tk.s) {
				if sub, _ := tfSubcommand(cmd, i, mode); sub != nil && isPassthrough(sub.s) && fa[tk.line] != "" {
					out = append(out, fa[tk.line])
				}
			}
		}
	}
	return out
}

// tfSubcommand finds the subcommand token after a terraform word at cmd[i], and
// the tokens after it.
func tfSubcommand(cmd []token, i int, mode tfMode) (*token, []token) {
	j := i + 1
	if mode == tfImage {
		for k := i + 1; k < len(cmd); k++ {
			if cmd[k].s == "--" && !cmd[k].quoted {
				j = k + 1
				break
			}
		}
	}
	for ; j < len(cmd); j++ {
		if cmd[j].redir {
			return nil, nil
		}
		if !strings.HasPrefix(cmd[j].s, "-") {
			return &cmd[j], cmd[j+1:]
		}
	}
	return nil, nil
}

func (t *tfScanner) check(rel, text string) []tfFinding {
	var out []tfFinding
	toks := lex(text, 1)
	lists := wordLists(toks)
	cmds := commands(toks)
	// for NAME in WORDS: NAME takes every word, so one terraform word makes NAME an
	// alias in this file (`for t in terraform; do $t apply; done`). Loop variables are
	// file-local: a generic name like img must not leak into other files.
	saved := t.vars
	t.vars = map[string]tfMode{}
	for k, v := range saved {
		t.vars[k] = v
	}
	defer func() { t.vars = saved }()
	for ci, cmd := range cmds {
		if lists[ci] == "" {
			continue
		}
		for _, w := range cmd {
			if m := t.modeOf(w.s); m != tfNone {
				t.vars[lists[ci]] = m
			}
		}
	}
	for ci, cmd := range cmds {
		if lists[ci] != "" {
			continue // a for/select word list is data, not a command
		}
		if !cmd[0].quoted && (cmd[0].s == "[[" || cmd[0].s == "[" || cmd[0].s == "test") {
			continue // a test expression compares words; it runs nothing
		}
		for i, tk := range cmd {
			if tk.redir || assignRe.MatchString(tk.s) || makeAssignRe.MatchString(tk.s) {
				continue
			}
			mode := t.modeOf(tk.s)
			if mode == tfNone {
				continue
			}
			sub, rest := tfSubcommand(cmd, i, mode)
			if sub == nil {
				continue
			}
			s := sub.s
			switch {
			case isPassthrough(s):
				if rel != tfPassthroughFile {
					out = append(out, tfFinding{sub.line, "terraform arguments passed through from \"$@\" (only scripts/ci/terraform.sh, which refuses cloud subcommands)"})
				}
			case strings.ContainsAny(s, "$`"):
				out = append(out, tfFinding{sub.line, "terraform subcommand " + s + " comes from a variable; write it literally"})
			case !tfAllowed[s]:
				out = append(out, tfFinding{sub.line, "terraform " + s + " is not allowed before Wave 5 (fmt, validate, version, init -backend=false, providers lock only)"})
			case s == "init" && !hasWord(rest, "-backend=false"):
				out = append(out, tfFinding{sub.line, "terraform init needs -backend=false before Wave 5"})
			case s == "providers" && !firstNonOption(rest, "lock"):
				out = append(out, tfFinding{sub.line, "terraform providers: only `providers lock` is allowed"})
			}
		}
	}
	return out
}

func hasWord(ts []token, w string) bool {
	for _, t := range ts {
		if t.s == w {
			return true
		}
	}
	return false
}

func firstNonOption(ts []token, w string) bool {
	for _, t := range ts {
		if !strings.HasPrefix(t.s, "-") {
			return t.s == w
		}
	}
	return false
}
