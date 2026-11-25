package nodeflags

import (
	"os/exec"
	"strings"
	"testing"
)

// The flag has to be one node knows: a build that passes --experimental-transform-types to a node that
// predates it fails every recording with an unknown-option error.
func TestTheFlagIsOneNodeKnows(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not on PATH")
	}
	out, err := exec.Command("node", "--help").Output()
	if err != nil {
		t.Skip("node --help failed")
	}
	flag := TypeScript()
	if flag == "" {
		t.Skip("this node cannot load TypeScript at all")
	}
	if !strings.Contains(string(out), strings.TrimPrefix(flag, "--")) {
		t.Errorf("node does not list %s in --help", flag)
	}
	for _, arg := range Args() {
		if strings.HasPrefix(arg, "--experimental-") && !strings.Contains(string(out), strings.TrimPrefix(arg, "--")) {
			t.Errorf("node does not know %s", arg)
		}
	}
}
