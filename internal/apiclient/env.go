package apiclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// GlobalsID is the environment that holds the global variables.
const GlobalsID = "globals"

// EnvStore keeps environments as JSON files in Dir (secret values are not written).
type EnvStore struct{ Dir string }

func (s EnvStore) st() Store { return Store{Dir: s.Dir} }

// List the environments by name; the globals are not listed.
func (s EnvStore) List() []Environment {
	ents, _ := os.ReadDir(s.Dir)
	var out []Environment
	for _, e := range ents {
		id := strings.TrimSuffix(e.Name(), ".json")
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || id == GlobalsID {
			continue
		}
		if v, err := s.Get(id); err == nil {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// Get an environment by id; the globals exist even before they are saved.
func (s EnvStore) Get(id string) (Environment, error) {
	p, err := s.st().path(id)
	if err != nil {
		return Environment{}, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if id == GlobalsID {
			return Environment{ID: GlobalsID, Name: "Globals", Vars: []EnvVar{}}, nil
		}
		return Environment{}, fmt.Errorf("no environment %s", id)
	}
	var e Environment
	if err := json.Unmarshal(b, &e); err != nil {
		return e, fmt.Errorf("environment %s: %w", id, err)
	}
	e.ID = id
	if e.Vars == nil {
		e.Vars = []EnvVar{}
	}
	return e, nil
}

// Find an environment by id or name (case-insensitive).
func (s EnvStore) Find(idOrName string) (Environment, error) {
	if e, err := s.Get(idOrName); err == nil {
		return e, nil
	}
	for _, e := range s.List() {
		if strings.EqualFold(e.Name, idOrName) {
			return e, nil
		}
	}
	return Environment{}, fmt.Errorf("no environment %q", idOrName)
}

// Save writes an environment (a new id from its name when it has none); secret values are left out.
func (s EnvStore) Save(e Environment) (Environment, error) {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return e, err
	}
	if strings.TrimSpace(e.Name) == "" {
		e.Name = "Environment"
	}
	if e.ID == "" {
		e.ID = s.st().NewID(e.Name)
		if e.ID == GlobalsID {
			e.ID = s.st().NewID(e.Name + "-env")
		}
	}
	p, err := s.st().path(e.ID)
	if err != nil {
		return e, err
	}
	out := e
	out.Vars = make([]EnvVar, 0, len(e.Vars))
	for _, v := range e.Vars {
		if strings.TrimSpace(v.Key) == "" {
			continue
		}
		if v.Secret {
			v.Value = ""
		}
		out.Vars = append(out.Vars, v)
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return e, err
	}
	return out, os.Rename(tmp, p)
}

// Delete an environment's file (the globals are emptied instead).
func (s EnvStore) Delete(id string) error {
	if id == GlobalsID {
		_, err := s.Save(Environment{ID: GlobalsID, Name: "Globals"})
		return err
	}
	p, err := s.st().path(id)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

// ImportEnvironment reads a Postman environment or globals export ({"name", "values": [...]}).
// Secret values come back in the result for the caller to keep in the credential store.
func ImportEnvironment(text, name string) (Environment, error) {
	var doc struct {
		Name   string `json:"name"`
		Scope  string `json:"_postman_variable_scope"`
		Values []struct {
			Key     string `json:"key"`
			Value   any    `json:"value"`
			Type    string `json:"type"`
			Enabled *bool  `json:"enabled"`
		} `json:"values"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(text), "\ufeff")), &doc); err != nil {
		return Environment{}, fmt.Errorf("not a Postman environment: %w", err)
	}
	if doc.Values == nil {
		return Environment{}, errors.New("not a Postman environment: no values")
	}
	e := Environment{Name: firstNonEmpty(name, doc.Name, "Environment"), Vars: []EnvVar{}}
	if doc.Scope == "globals" {
		e.ID, e.Name = GlobalsID, "Globals"
	}
	for _, v := range doc.Values {
		if v.Key == "" {
			continue
		}
		val := ""
		if v.Value != nil {
			val = fmt.Sprint(v.Value)
		}
		e.Vars = append(e.Vars, EnvVar{Key: v.Key, Value: val, Secret: v.Type == "secret", Off: v.Enabled != nil && !*v.Enabled})
	}
	return e, nil
}

// IsEnvironment says whether a document is a Postman environment or globals export.
func IsEnvironment(text string) bool {
	var doc map[string]any
	if json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(text), "\ufeff")), &doc) != nil {
		return false
	}
	_, vals := doc["values"].([]any)
	_, items := doc["item"]
	return vals && !items
}

// ExportEnvironment writes an environment as a Postman environment (secret values left empty).
func ExportEnvironment(e Environment) []byte {
	type v struct {
		Key     string `json:"key"`
		Value   string `json:"value"`
		Type    string `json:"type"`
		Enabled bool   `json:"enabled"`
	}
	vals := []v{}
	for _, x := range e.Vars {
		t, val := "default", x.Value
		if x.Secret {
			t, val = "secret", ""
		}
		vals = append(vals, v{x.Key, val, t, !x.Off})
	}
	scope := "environment"
	if e.ID == GlobalsID {
		scope = "globals"
	}
	b, _ := json.MarshalIndent(map[string]any{"name": e.Name, "values": vals, "_postman_variable_scope": scope}, "", "  ")
	return b
}
