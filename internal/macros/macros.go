// Package macros reads a printer's gcode macros: names, descriptions and
// the parameters their templates use, the way Mainsail does — Klipper
// doesn't publish macro parameters, so they're found in the template
// text (params.X, params['X'], params.get('X'), with |default(…)).
package macros

import (
	"regexp"
	"sort"
	"strings"
)

// Param is one parameter a macro's template reads.
type Param struct {
	Name    string // as written in the template, upper-cased (Klipper's form)
	Default string // from |default(…) or .get('X', …), "" when none
}

// Macro is one runnable gcode_macro.
type Macro struct {
	Name        string // the command, upper-case: "PRINT_START"
	Description string // "" when Klipper's placeholder "G-Code macro"
	Params      []Param
}

// List builds the printer's macros from its object list and configfile
// settings (lower-cased section names). Macros named _something are
// helpers by convention and left out, as Mainsail does.
func List(objects []string, settings map[string]any) []Macro {
	var out []Macro
	for _, obj := range objects {
		name, ok := strings.CutPrefix(obj, "gcode_macro ")
		if !ok || name == "" || strings.HasPrefix(name, "_") {
			continue
		}
		mc := Macro{Name: strings.ToUpper(name)}
		if sec, ok := settings[strings.ToLower(obj)].(map[string]any); ok {
			if d, _ := sec["description"].(string); d != "" && d != "G-Code macro" {
				mc.Description = d
			}
			if g, _ := sec["gcode"].(string); g != "" {
				mc.Params = Params(g)
			}
		}
		out = append(out, mc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

var (
	// params.NAME, optionally |default(VALUE)
	dotRe = regexp.MustCompile(`params\.([A-Za-z_][A-Za-z0-9_]*)(\s*\|\s*default\(\s*([^)]*?)\s*\))?`)
	// params['NAME'] / params["NAME"], optionally |default(VALUE)
	idxRe = regexp.MustCompile(`params\[\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]\s*\](\s*\|\s*default\(\s*([^)]*?)\s*\))?`)
	// params.get('NAME'[, VALUE])
	getRe = regexp.MustCompile(`params\.get\(\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]\s*(,\s*([^)]*?))?\s*\)`)
)

// dict methods that look like parameters after "params.".
var notParams = map[string]bool{"GET": true, "ITEMS": true, "KEYS": true, "VALUES": true}

// Params finds the parameters a macro template reads, in order of first
// use, each with the first default given for it.
func Params(template string) []Param {
	var out []Param
	seen := map[string]int{}
	add := func(name, def string) {
		name = strings.ToUpper(name)
		if notParams[name] {
			return
		}
		def = unquote(def)
		if i, ok := seen[name]; ok {
			if out[i].Default == "" {
				out[i].Default = def
			}
			return
		}
		seen[name] = len(out)
		out = append(out, Param{Name: name, Default: def})
	}
	type hit struct {
		at        int
		name, def string
	}
	var hits []hit
	for _, re := range []*regexp.Regexp{getRe, dotRe, idxRe} {
		for _, m := range re.FindAllStringSubmatchIndex(template, -1) {
			h := hit{at: m[0], name: template[m[2]:m[3]]}
			if m[6] >= 0 {
				h.def = template[m[6]:m[7]]
			}
			hits = append(hits, h)
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].at < hits[j].at })
	for _, h := range hits {
		add(h.name, h.def)
	}
	return out
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
