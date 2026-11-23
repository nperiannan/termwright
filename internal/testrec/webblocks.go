package testrec

import (
	"fmt"
	"sort"
	"strings"

	"github.com/termwright/termwright/internal/webrun"
)

// A web block is a procedure whose only step is a web step: "block" actions are replaced by its actions
// (its page first, when it has one), so the actions go on in the same browser.

// ExpandBlocks replaces "block" actions by the blocks' actions, their {{params}} filled from with.
func (f Folder) ExpandBlocks(acts []WebAction) ([]WebAction, error) { return f.expandBlocks(acts, 0) }

func (f Folder) expandBlocks(acts []WebAction, depth int) ([]WebAction, error) {
	if depth > 8 {
		return nil, fmt.Errorf("blocks use each other more than 8 deep")
	}
	var out []WebAction
	for i, a := range acts {
		if !strings.EqualFold(a.Do, "block") {
			out = append(out, a)
			continue
		}
		p, err := f.LoadProc(a.Target)
		if err != nil {
			return nil, fmt.Errorf("action %d: %v", i+1, err)
		}
		if len(p.Steps) != 1 || p.Steps[0].Web == nil {
			return nil, fmt.Errorf("action %d: %s is not a block of web actions", i+1, p.Name)
		}
		vals := map[string]string{}
		for _, prm := range p.Params {
			if k, d, ok := strings.Cut(prm, "="); ok {
				vals[strings.TrimSpace(k)] = strings.TrimSpace(d)
			}
		}
		for k, v := range a.With {
			vals[k] = v
		}
		for _, prm := range p.Params {
			k, _, _ := strings.Cut(prm, "=")
			if _, ok := vals[strings.TrimSpace(k)]; !ok {
				return nil, fmt.Errorf("action %d: block %s needs %s", i+1, p.Name, strings.TrimSpace(k))
			}
		}
		st := subst(p.Steps[0], vals)
		var inner []WebAction
		if st.Web.URL != "" {
			inner = append(inner, WebAction{Do: "goto", Value: st.Web.URL})
		}
		inner = append(inner, st.Web.Actions...)
		if inner, err = f.expandBlocks(inner, depth+1); err != nil {
			return nil, err
		}
		out = append(out, inner...)
	}
	return out, nil
}

// expandStepBlocks expands the blocks of a step's web actions (loops too).
func (f Folder) expandStepBlocks(l []Step) error {
	for i := range l {
		if l[i].Loop != nil {
			if err := f.expandStepBlocks(l[i].Loop.Steps); err != nil {
				return err
			}
		}
		if l[i].Web == nil {
			continue
		}
		acts, err := f.ExpandBlocks(l[i].Web.Actions)
		if err != nil {
			return fmt.Errorf("step %d: %v", i+1, err)
		}
		w := *l[i].Web
		w.Actions = acts
		l[i].Web = &w
	}
	return nil
}

// SaveWebBlock saves actions as a block (procs/<name>.yaml); its parameters are the {{names}} the actions use
// (not username and password, which come from the web app). It returns the file.
func (f Folder) SaveWebBlock(name, description string, acts []WebAction) (string, error) {
	seen := map[string]bool{}
	for _, a := range acts {
		for _, s := range []string{a.Target, a.Value} {
			for _, m := range varRef.FindAllStringSubmatch(s, -1) {
				if k := m[1]; k != "username" && k != "password" {
					seen[k] = true
				}
			}
		}
	}
	var params []string
	for k := range seen {
		params = append(params, k)
	}
	sort.Strings(params)
	clean := make([]WebAction, len(acts))
	for i, a := range acts {
		a.Secret = false
		clean[i] = a
	}
	return f.SaveProc(name, Proc{Name: name, Description: description, Params: params, Steps: []Step{{Web: &webrun.Step{Actions: clean}}}})
}
