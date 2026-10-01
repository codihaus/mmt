package i18n

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// Every string passed to tr()/i18n.T() must have a Vietnamese entry.
func TestCatalogCoversSource(t *testing.T) {
	re := regexp.MustCompile(`(?:\btr|i18n\.T)\(("(?:[^"\\]|\\.)*")\)`)
	files, _ := filepath.Glob("../ui/*.go")
	bg, _ := filepath.Glob("../background/*.go")
	files = append(files, bg...)
	files = append(files, "../../main.go", "../launcher/launcher.go")
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllSubmatch(src, -1) {
			s, err := strconv.Unquote(string(m[1]))
			if err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			if _, ok := vi[s]; !ok {
				t.Errorf("%s: missing vi translation for %q", filepath.Base(f), s)
			}
		}
	}
}

func TestDetect(t *testing.T) {
	for _, v := range []string{"MMT_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		t.Setenv(v, "")
	}
	if got := Detect("vi"); got != "vi" {
		t.Errorf("configured language ignored: %q", got)
	}
	t.Setenv("LANG", "vi_VN.UTF-8")
	if got := Detect(""); got != "vi" {
		t.Errorf("LANG=vi_VN: got %q", got)
	}
	t.Setenv("MMT_LANG", "en")
	if got := Detect("vi"); got != "en" {
		t.Errorf("MMT_LANG should win over the config: got %q", got)
	}
	if got := Detect(""); got != "en" {
		t.Errorf("MMT_LANG should win over LANG: got %q", got)
	}
}

func TestT(t *testing.T) {
	defer Set("en")
	Set("vi")
	if got := T("Loading…"); got != "Đang tải…" {
		t.Errorf("vi: %q", got)
	}
	if got := T("not in catalog"); got != "not in catalog" {
		t.Errorf("fallback: %q", got)
	}
	Set("xx")
	if Lang() != "en" {
		t.Errorf("unknown language should fall back to en, got %q", Lang())
	}
}
