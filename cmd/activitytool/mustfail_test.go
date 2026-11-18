package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMustFailFixtures runs every test file in testdata/must-fail and requires
// each to fail (exit 1) with the message in its "# want:" header lines. It
// proves the tool cannot pass a broken policy or a wrong expectation.
func TestMustFailFixtures(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "must-fail", "*.test.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no must-fail fixtures found")
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			wants := wantLines(t, file)
			if len(wants) == 0 {
				t.Fatalf("%s has no '# want:' header line", file)
			}

			var stdout, stderr bytes.Buffer
			code := execute([]string{"test", file}, &stdout, &stderr)
			out := stdout.String() + stderr.String()
			if code != 1 {
				t.Fatalf("expected exit 1, got %d:\n%s", code, out)
			}
			for _, want := range wants {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q:\n%s", want, out)
				}
			}
		})
	}
}

// wantLines returns the text after "# want:" on the file's leading comment lines.
func wantLines(t *testing.T, file string) []string {
	t.Helper()
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var wants []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "#") {
			break
		}
		if want, ok := strings.CutPrefix(line, "# want:"); ok {
			wants = append(wants, strings.TrimSpace(want))
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return wants
}
