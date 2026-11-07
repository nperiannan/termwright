package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"0.29.0", "0.30.0", -1},
		{"0.30.0", "0.30.0", 0},
		{"0.30.0-dev~git1234567", "0.30.0", -1},
		{"0.30.0", "0.30.0-dev", 1},
		{"0.30.0~git1234567", "0.30.0-dev~gitabcdef0", 0},
		{"0.31.0-dev~git1", "0.30.0", 1},
		{"v0.30.1", "0.30.0", 1},
		{"dev", "0.30.0", -1},
		{"", "", 0},
		{"1.0", "0.30.0", 1},
	} {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestSameNumbers(t *testing.T) {
	if !sameNumbers("0.30.0", "0.30.0~git1234567") || sameNumbers("0.30.1", "0.30.0~git1") || sameNumbers("", "") {
		t.Fatal("sameNumbers")
	}
}

func TestParseContainer(t *testing.T) {
	spec := "bridge\nP||7443|7443/tcp\nP|::|7443|7443/tcp\nM|volume|termwright-team-lab||/data\nE|PATH=/usr/bin\nE|TW_TEAM_NAME=Lab B|x\nE|TZ=Asia/Kolkata\n" +
		"R|always|0\nL|536870912|1500000000\nB|owner=lab ops\nB|org.opencontainers.image.source=x\n"
	c := parseContainer(spec)
	got := strings.Join(c.args, " ")
	want := `-p '7443:7443' -v 'termwright-team-lab:/data' -e 'TW_TEAM_NAME=Lab B|x' -e 'TZ=Asia/Kolkata' --memory 536870912 --cpus 1.5 --label 'owner=lab ops' --restart always`
	if got != want || !c.data || c.compose.project != "" {
		t.Fatalf("parseContainer =\n %q, %v\nwant\n %q", got, c.data, want)
	}
	c = parseContainer("host\nM|bind||/srv/team|/data\nE|TW_LISTEN=:7450\nR|on-failure|3\nL|0|0\n")
	if got := strings.Join(c.args, " "); got != `--network host -v '/srv/team:/data' -e 'TW_LISTEN=:7450' --restart on-failure:3` || !c.data {
		t.Fatalf("host network: %q %v", got, c.data)
	}
	if c := parseContainer("bridge\nP|10.0.0.5|7443|7443/tcp\nR|no|0\n"); c.data || strings.Join(c.args, " ") != `-p '10.0.0.5:7443:7443'` {
		t.Fatalf("no volume: %+v", c)
	}
	c = parseContainer("lab_default\nB|com.docker.compose.project=lab\nB|com.docker.compose.project.working_dir=/opt/team\nB|com.docker.compose.project.config_files=/opt/team/docker-compose.yml\nB|com.docker.compose.service=team\n")
	if c.compose.project != "lab" || c.compose.dir != "/opt/team" || c.compose.service != "team" || strings.Contains(strings.Join(c.args, " "), "compose") {
		t.Fatalf("compose: %+v", c)
	}
}

func TestPemFingerprint(t *testing.T) {
	if pemFingerprint("no certificate") != "" {
		t.Fatal("a fingerprint of nothing")
	}
}

// TestRunArgsDocker reads a running container (TW_TEST_CONTAINER) with the
// inspect format, prints the docker run arguments for it.
func TestRunArgsDocker(t *testing.T) {
	name := os.Getenv("TW_TEST_CONTAINER")
	if name == "" {
		t.Skip("set TW_TEST_CONTAINER to a team server container")
	}
	b, err := exec.Command("sh", "-c", "docker inspect -f "+shQuote(inspectFormat)+" "+name).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	c := parseContainer(string(b))
	if !c.data {
		t.Fatal("no /data")
	}
	fmt.Println("ARGS", strings.Join(c.args, " "))
	if fp := os.Getenv("TW_TEST_FP"); fp != "" {
		pem, _ := exec.Command("sh", "-c", "docker cp "+name+":/data/cert.pem -").CombinedOutput()
		if got := pemFingerprint(string(pem)); got != fp {
			t.Fatalf("fingerprint %q, want %q", got, fp)
		}
	}
}
