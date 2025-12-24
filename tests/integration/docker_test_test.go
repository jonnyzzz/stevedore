package integration_test

import "testing"

func TestDockerStack(t *testing.T) {
	donor := NewTestContainer(t, "Dockerfile.ubuntu")
	donor.ExecOK("uname", "-a")
}
