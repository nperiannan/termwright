package testrec

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type flakyDrv struct {
	fake
	n     int
	sleep time.Duration
}

func (f *flakyDrv) Command(ctx context.Context, s, cmd, p string) (string, error) {
	if f.sleep > 0 {
		select {
		case <-time.After(f.sleep):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	f.n++
	if cmd == "show flaky" {
		if f.n == 1 {
			return "", errors.New("no answer")
		}
		return "ok", nil
	}
	return f.fake.Command(ctx, s, cmd, p)
}

func TestRerunAndTimeouts(t *testing.T) {
	f := Folder{Root: t.TempDir()}
	f.Init()
	f.Save("flaky", Case{Name: "flaky", Steps: []Step{{Run: &RunStep{Session: "s", Command: "show flaky"}}}})
	rep, err := f.RunFilesWith(context.Background(), []string{"cases/flaky.yaml"}, &flakyDrv{}, nil, RunOptions{Rerun: 2}, nil)
	if err != nil || len(rep.Results) != 1 || !rep.Results[0].Passed || !rep.Results[0].Flaky || rep.Results[0].Attempts != 2 {
		t.Fatalf("%v %+v", err, rep.Results)
	}
	slow := Case{Name: "slow", Timeout: 1, Steps: []Step{{Run: &RunStep{Session: "s", Command: "show version"}}}, Cleanup: []Step{{Run: &RunStep{Session: "s", Command: "show vlan"}}}}
	d := &flakyDrv{sleep: 1500 * time.Millisecond}
	r := RunWith(context.Background(), slow, d, nil, RunOptions{})
	if r.Passed || !strings.Contains(r.Error, "longer than 1 s") || r.Steps[len(r.Steps)-1].Error != "" && d.n == 0 {
		t.Fatalf("%+v", r)
	}
}
