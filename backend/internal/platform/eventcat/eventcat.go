// Package eventcat loads the product event catalogue (05 §2.6, D-50) from the
// per-domain files in shared/telemetry/events/<domain>.yaml and validates an
// event's props against it. The same files generate the TypeScript emitter
// types (backend/internal/devtools/eventgen), so client and server agree.
//
// A prop is one of: enum (a fixed value list), int, number, bool, ulid, or
// token (a short [a-z0-9_.:-] string, for ids such as a feature or model name).
// There is deliberately no free-text type: free text is never an event prop.
package eventcat

import (
	"bytes"
	"fmt"
	"io/fs"
	"math"
	"path"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Prop types.
const (
	TypeEnum   = "enum"
	TypeInt    = "int"
	TypeNumber = "number"
	TypeBool   = "bool"
	TypeULID   = "ulid"
	TypeToken  = "token"
)

// Emitters.
const (
	EmitterClient = "client"
	EmitterServer = "server"
)

// Prop describes one event property.
type Prop struct {
	Type     string   `yaml:"type"`
	Values   []string `yaml:"values,omitempty"`
	Min      *float64 `yaml:"min,omitempty"`
	Max      *float64 `yaml:"max,omitempty"`
	Optional bool     `yaml:"optional,omitempty"`
}

// Event is one catalogue entry.
type Event struct {
	Name        string
	Domain      string
	Owner       string
	Emitter     string
	Description string
	Props       map[string]Prop
}

type file struct {
	Domain string `yaml:"domain"`
	Owner  string `yaml:"owner"`
	Events map[string]struct {
		Emitter     string          `yaml:"emitter"`
		Description string          `yaml:"description"`
		Props       map[string]Prop `yaml:"props"`
	} `yaml:"events"`
}

// Catalogue is every event, by name.
type Catalogue struct{ events map[string]*Event }

var (
	reName   = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	reProp   = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	reDomain = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	reULID   = regexp.MustCompile(`^[0-7][0-9A-HJKMNP-TV-Z]{25}$`)
	reToken  = regexp.MustCompile(`^[a-z0-9][a-z0-9_.:-]{0,63}$`)
	reValue  = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,31}$`)
)

// Load reads every *.yaml in the root of fsys. It fails on a malformed file, an
// unknown field, a bad name, a prop type that is not allowed, and an event
// name defined in two files (the build fails, 05 §2.6).
func Load(fsys fs.FS) (*Catalogue, error) {
	names, err := fs.Glob(fsys, "*.yaml")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	c := &Catalogue{events: map[string]*Event{}}
	owner := map[string]string{}
	for _, n := range names {
		b, err := fs.ReadFile(fsys, n)
		if err != nil {
			return nil, err
		}
		var f file
		dec := yaml.NewDecoder(bytes.NewReader(b))
		dec.KnownFields(true)
		if err := dec.Decode(&f); err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		if want := strings.TrimSuffix(path.Base(n), ".yaml"); f.Domain != want {
			return nil, fmt.Errorf("%s: domain %q must equal the file name %q", n, f.Domain, want)
		}
		if !reDomain.MatchString(f.Domain) || f.Owner == "" {
			return nil, fmt.Errorf("%s: domain and owner are required", n)
		}
		for name, e := range f.Events {
			if !reName.MatchString(name) {
				return nil, fmt.Errorf("%s: event name %q must be snake_case", n, name)
			}
			if prev, dup := owner[name]; dup {
				return nil, fmt.Errorf("%s: event %q is already defined in %s", n, name, prev)
			}
			if e.Emitter != EmitterClient && e.Emitter != EmitterServer {
				return nil, fmt.Errorf("%s: event %q: emitter must be client or server", n, name)
			}
			for pn, p := range e.Props {
				if err := checkProp(pn, p); err != nil {
					return nil, fmt.Errorf("%s: event %q: %w", n, name, err)
				}
			}
			owner[name] = n
			c.events[name] = &Event{Name: name, Domain: f.Domain, Owner: f.Owner, Emitter: e.Emitter, Description: e.Description, Props: e.Props}
		}
	}
	return c, nil
}

func checkProp(name string, p Prop) error {
	if !reProp.MatchString(name) {
		return fmt.Errorf("prop %q must be snake_case", name)
	}
	switch p.Type {
	case TypeEnum:
		if len(p.Values) == 0 {
			return fmt.Errorf("prop %q: enum needs values", name)
		}
		for _, v := range p.Values {
			if !reValue.MatchString(v) {
				return fmt.Errorf("prop %q: enum value %q must match %s", name, v, reValue)
			}
		}
	case TypeInt, TypeNumber, TypeBool, TypeULID, TypeToken:
		if len(p.Values) > 0 {
			return fmt.Errorf("prop %q: values only apply to enum", name)
		}
	default:
		return fmt.Errorf("prop %q: type %q is not allowed (enum, int, number, bool, ulid, token; free text is never a prop)", name, p.Type)
	}
	return nil
}

// Event returns the event named name.
func (c *Catalogue) Event(name string) (*Event, bool) {
	e, ok := c.events[name]
	return e, ok
}

// Names returns every event name, sorted.
func (c *Catalogue) Names() []string {
	out := make([]string, 0, len(c.events))
	for n := range c.events {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Len is the number of events.
func (c *Catalogue) Len() int { return len(c.events) }

// MaxIntBound is the default bound of an int or number prop without min/max.
const MaxIntBound = 1e9

// Validate checks props against the event. The error names the prop and the
// rule, never the value the client sent.
func (e *Event) Validate(props map[string]any) error {
	for name := range props {
		if _, ok := e.Props[name]; !ok {
			return fmt.Errorf("props.%s: not a prop of %s", name, e.Name)
		}
	}
	names := make([]string, 0, len(e.Props))
	for n := range e.Props {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		p := e.Props[name]
		v, present := props[name]
		if !present || v == nil {
			if p.Optional {
				continue
			}
			return fmt.Errorf("props.%s: required", name)
		}
		if err := checkValue(p, v); err != nil {
			return fmt.Errorf("props.%s: %w", name, err)
		}
	}
	return nil
}

func checkValue(p Prop, v any) error {
	switch p.Type {
	case TypeEnum:
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("must be a string")
		}
		for _, x := range p.Values {
			if s == x {
				return nil
			}
		}
		return fmt.Errorf("not one of the allowed values")
	case TypeInt, TypeNumber:
		f, ok := v.(float64)
		if !ok {
			if i, isInt := v.(int); isInt {
				f, ok = float64(i), true
			}
		}
		if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("must be a number")
		}
		if p.Type == TypeInt && f != math.Trunc(f) {
			return fmt.Errorf("must be a whole number")
		}
		lo, hi := -MaxIntBound, MaxIntBound
		if p.Min != nil {
			lo = *p.Min
		}
		if p.Max != nil {
			hi = *p.Max
		}
		if f < lo || f > hi {
			return fmt.Errorf("out of range")
		}
	case TypeBool:
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("must be a boolean")
		}
	case TypeULID:
		if s, ok := v.(string); !ok || !reULID.MatchString(s) {
			return fmt.Errorf("must be a ULID")
		}
	case TypeToken:
		if s, ok := v.(string); !ok || !reToken.MatchString(s) {
			return fmt.Errorf("must be a short token ([a-z0-9_.:-], at most 64)")
		}
	}
	return nil
}
