package cmd

import (
	"bytes"
	"testing"
)

func TestHelloCmdPrintsHelloWorld(t *testing.T) {
	var buf bytes.Buffer
	helloCmd.SetOut(&buf)
	t.Cleanup(func() { helloCmd.SetOut(nil) })

	helloCmd.Run(helloCmd, nil)

	got := buf.String()
	want := "hello world\n"
	if got != want {
		t.Fatalf("hello output = %q, want %q", got, want)
	}
}
