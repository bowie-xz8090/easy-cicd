package deployer

import "testing"

func TestShellQuote(t *testing.T) {
	tests := map[string]string{
		"/data/temp":          "'/data/temp'",
		"/data/it's temp":     "'/data/it'\"'\"'s temp'",
		"artifact.tar.gz":     "'artifact.tar.gz'",
		"/path/with spaces/x": "'/path/with spaces/x'",
	}
	for input, want := range tests {
		if got := shellQuote(input); got != want {
			t.Fatalf("shellQuote(%q) = %q, want %q", input, got, want)
		}
	}
}
