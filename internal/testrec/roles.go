package testrec

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Roles is the team's roles.yaml in the tests folder. The lists hold user names (as the computer knows
// them, as in "Run by"); an empty list lets everyone. The folder is shared through Git, so this keeps an
// honest team in step - it is not a security boundary.
type Roles struct {
	// Editors may save, rename and remove tests; Runners may run them; Approvers approve runs.
	Editors   []string `yaml:"editors,omitempty" json:"editors,omitempty"`
	Runners   []string `yaml:"runners,omitempty" json:"runners,omitempty"`
	Approvers []string `yaml:"approvers,omitempty" json:"approvers,omitempty"`
	// Approval: "production" = runs that touch production sessions need an approved request, "always" = every
	// run with a testbed, "" = none.
	Approval string `yaml:"approval,omitempty" json:"approval,omitempty"`
}

// ErrNeedsApproval: the run has to be approved first (RequestApproval).
var ErrNeedsApproval = errors.New("this run needs an approval")

// Me is the user name runs and approvals are recorded under.
func Me() string { u, _ := me(); return u }

// Roles reads roles.yaml (empty when there is none).
func (f Folder) Roles() (Roles, error) {
	var r Roles
	b, err := os.ReadFile(filepath.Join(f.Root, "roles.yaml"))
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	if err := yaml.Unmarshal(b, &r); err != nil {
		return r, fmt.Errorf("roles.yaml: %v", err)
	}
	return r, nil
}

func inList(l []string, u string) bool {
	return len(l) == 0 || slices.ContainsFunc(l, func(x string) bool { return strings.EqualFold(strings.TrimSpace(x), u) || x == "*" })
}

// May reports whether the user may do an action ("edit", "run", "approve"); an error says why not.
func (r Roles) May(user, action string) error {
	var l []string
	switch action {
	case "edit":
		l = r.Editors
	case "run":
		l = r.Runners
	case "approve":
		l = r.Approvers
	}
	if inList(l, user) {
		return nil
	}
	return fmt.Errorf("%s may not %s tests here (roles.yaml: %s)", user, action, strings.Join(l, ", "))
}

// Approval is a request to run tests that need one, in approvals/.
type Approval struct {
	ID          string    `yaml:"id" json:"id"`
	Files       []string  `yaml:"files" json:"files"`
	Testbed     string    `yaml:"testbed,omitempty" json:"testbed,omitempty"`
	Reason      string    `yaml:"reason,omitempty" json:"reason,omitempty"`
	RequestedBy string    `yaml:"requestedBy" json:"requestedBy"`
	At          time.Time `yaml:"at" json:"at"`
	ApprovedBy  string    `yaml:"approvedBy,omitempty" json:"approvedBy,omitempty"`
	ApprovedAt  time.Time `yaml:"approvedAt,omitempty" json:"approvedAt,omitempty"`
	Rejected    bool      `yaml:"rejected,omitempty" json:"rejected,omitempty"`
	Note        string    `yaml:"note,omitempty" json:"note,omitempty"`
	// Expired: approved more than ApprovalValid ago (computed).
	Expired bool `yaml:"-" json:"expired,omitempty"`
}

// ApprovalValid is how long an approval allows its run.
const ApprovalValid = 24 * time.Hour

func sameFiles(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	sort.Strings(x)
	sort.Strings(y)
	return slices.Equal(x, y)
}

// RequestApproval saves a request for the run (files on testbed).
func (f Folder) RequestApproval(files []string, testbed, reason string) (Approval, error) {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	a := Approval{ID: time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b), Files: files, Testbed: testbed, Reason: reason, RequestedBy: Me(), At: time.Now()}
	if err := f.saveApproval(a); err != nil {
		return a, err
	}
	f.Audit("approval requested", a.ID+" "+strings.Join(files, ", ")+" on "+orDash(testbed))
	return a, nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func (f Folder) saveApproval(a Approval) error {
	dir := filepath.Join(f.Root, "approvals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, _ := yaml.Marshal(a)
	return os.WriteFile(filepath.Join(dir, a.ID+".yaml"), b, 0o644)
}

// Approvals lists the requests, newest first.
func (f Folder) Approvals() ([]Approval, error) {
	ents, err := os.ReadDir(filepath.Join(f.Root, "approvals"))
	if errors.Is(err, os.ErrNotExist) {
		return []Approval{}, nil
	}
	out := []Approval{}
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(f.Root, "approvals", e.Name()))
		if err != nil {
			continue
		}
		var a Approval
		if yaml.Unmarshal(b, &a) == nil && a.ID != "" {
			a.Expired = !a.ApprovedAt.IsZero() && time.Since(a.ApprovedAt) > ApprovalValid
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out, err
}

// Decide approves (or rejects) a request; the approver must be one of the approvers and not the requester.
func (f Folder) Decide(id string, approve bool, note string) (Approval, error) {
	r, err := f.Roles()
	if err != nil {
		return Approval{}, err
	}
	u := Me()
	if err := r.May(u, "approve"); err != nil {
		return Approval{}, err
	}
	l, _ := f.Approvals()
	for _, a := range l {
		if a.ID != id {
			continue
		}
		if strings.EqualFold(a.RequestedBy, u) && len(r.Approvers) > 0 {
			return a, errors.New("someone else has to approve your own request")
		}
		a.ApprovedBy, a.ApprovedAt, a.Rejected, a.Note = u, time.Now(), !approve, note
		if err := f.saveApproval(a); err != nil {
			return a, err
		}
		act := "approved"
		if !approve {
			act = "rejected"
		}
		f.Audit("run "+act, a.ID+" (requested by "+a.RequestedBy+")")
		return a, nil
	}
	return Approval{}, errors.New("no approval request " + id)
}

// NeedsApproval reports whether a run needs an approval under the roles: prod = it touches production sessions.
func (r Roles) NeedsApproval(prod bool, testbed string) bool {
	switch r.Approval {
	case "always":
		return true
	case "production":
		return prod
	}
	return false
}

// Approved finds an approval that allows this run: the same tests and testbed, approved and not expired.
func (f Folder) Approved(files []string, testbed string) (Approval, bool) {
	l, _ := f.Approvals()
	for _, a := range l {
		if a.ApprovedBy != "" && !a.Rejected && !a.Expired && a.Testbed == testbed && sameFiles(a.Files, files) {
			return a, true
		}
	}
	return Approval{}, false
}
