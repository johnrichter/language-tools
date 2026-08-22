package pin

import (
	"path/filepath"
	"testing"
)

func TestFloorFor(t *testing.T) {
	cases := []struct {
		language string
		fixture  string
		want     string
	}{
		{"go", "fixture1", "1.20"},
		{"go", "fixture2", "1.24"},
		{"rust", "fixture1", "1.70"},
		{"rust", "fixture2", "1.85"},
		{"python", "fixture1", "3.10"},
		{"python", "fixture2", "3.12"},
	}
	for _, c := range cases {
		dir := filepath.Join("testdata", c.language, c.fixture)
		got, ok, err := floorFor(dir, c.language)
		if err != nil {
			t.Fatalf("floorFor(%s, %s): %v", dir, c.language, err)
		}
		if !ok {
			t.Fatalf("floorFor(%s, %s): ok = false, want true", dir, c.language)
		}
		if got != c.want {
			t.Errorf("floorFor(%s, %s) = %q, want %q", dir, c.language, got, c.want)
		}
	}
}

func TestFloorForUnknownLanguage(t *testing.T) {
	_, ok, err := floorFor(filepath.Join("testdata", "go", "fixture1"), "cobol")
	if err != nil {
		t.Fatalf("floorFor: %v", err)
	}
	if ok {
		t.Error("floorFor for an unrecognized language: ok = true, want false")
	}
}
