package i18n_test

// The validator for the converter's catalogues. A catalogue that is short of
// a key, holds an empty value, or spells a placeholder differently from the
// English one is refused here — before it reaches a person as a blank
// button, an English word on a German screen, or "{n} files" with the {n}
// left in.

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/brf-tech/filex-convert/internal/formats"
	"github.com/brf-tech/filex-convert/internal/i18n"
	"github.com/brf-tech/filex-convert/internal/job"
)

func TestEveryKeyIsInEveryLanguage(t *testing.T) {
	en := i18n.Catalogue("en")
	if len(en) < 100 {
		t.Fatalf("the English catalogue holds %d keys; it did not load", len(en))
	}
	for _, lang := range i18n.Langs {
		cat := i18n.Catalogue(lang)
		for k := range en {
			if _, ok := cat[k]; !ok {
				t.Errorf("%s: %q is missing", lang, k)
			}
		}
		for k := range cat {
			if _, ok := en[k]; !ok {
				t.Errorf("%s: %q is not in the English catalogue (a typo, or a key nobody reads)", lang, k)
			}
		}
	}
}

func TestNoValueIsEmpty(t *testing.T) {
	for _, lang := range i18n.Langs {
		for k, v := range i18n.Catalogue(lang) {
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s: %q is empty", lang, k)
			}
		}
	}
}

func TestPlaceholdersAreTheEnglishOnes(t *testing.T) {
	en := i18n.Catalogue("en")
	for _, lang := range i18n.Langs[1:] {
		for k, v := range i18n.Catalogue(lang) {
			want, got := strings.Join(i18n.Placeholders(en[k]), " "), strings.Join(i18n.Placeholders(v), " ")
			if want != got {
				t.Errorf("%s: %q has placeholders [%s], English has [%s]", lang, k, got, want)
			}
		}
	}
}

// The register and the typography the filex language packs use: a French
// value carries a no-break space before a colon and the typographic
// apostrophe; a Spanish question opens with ¿; German quotes are not the
// English ones.
func TestTheLanguagesKeepTheirTypography(t *testing.T) {
	for k, v := range i18n.Catalogue("fr") {
		if strings.Contains(v, "'") {
			t.Errorf("fr: %q uses ' where French writes ’: %q", k, v)
		}
		if regexp.MustCompile(`[^\x{00A0}\s\d]:(\s|$)`).MatchString(v) || strings.Contains(v, " :") {
			t.Errorf("fr: %q has a colon without its no-break space: %q", k, v)
		}
		if regexp.MustCompile(`[^\x{202F}\s][?!;]`).MatchString(v) {
			t.Errorf("fr: %q has ? ! or ; without its narrow no-break space: %q", k, v)
		}
	}
	for k, v := range i18n.Catalogue("es") {
		if strings.HasSuffix(v, "?") && !strings.Contains(v, "¿") {
			t.Errorf("es: %q asks without ¿: %q", k, v)
		}
	}
	for k, v := range i18n.Catalogue("de") {
		if strings.ContainsAny(v, "“”") {
			t.Errorf("de: %q uses English quotation marks: %q", k, v)
		}
		if strings.Contains(v, " — ") {
			t.Errorf("de: %q uses the English spaced em dash; German writes \" – \": %q", k, v)
		}
	}
}

// A message whose words are still the English ones in another language is
// the half-translated screen these catalogues exist to prevent. A few values
// are rightly identical (a name, a number, "Format" in German).
func TestNothingIsLeftInEnglish(t *testing.T) {
	// The words each language rightly shares with English: names, numbers,
	// patterns of placeholders, and cognates ("Audio", "Format", "Document").
	same := map[string][]string{
		"es": {"category.audio", "category.video", "view.no"},
		"de": {"category.audio", "category.text", "category.video", "view.blocked.format", "view.step.format"},
		"fr": {"category.archive", "category.audio", "category.document", "category.image", "option.icon_sizes.app",
			"option.pages.label", "view.blocked.format", "view.step.format"},
	}
	everywhere := []string{"error.with_detail", "error.with_file", "option.bitrate.rate", "option.icon_sizes.favicon"}
	en := i18n.Catalogue("en")
	for _, lang := range []string{"es", "de", "fr"} {
		ok := map[string]bool{}
		for _, k := range append(same[lang], everywhere...) {
			ok[k] = true
		}
		for k, v := range i18n.Catalogue(lang) {
			if v == en[k] && !ok[k] {
				t.Errorf("%s: %q is the English text %q", lang, k, v)
			}
		}
	}
}

// Every key the code asks for exists, and every key in the catalogue is
// asked for somewhere — a key nobody reads is a translation somebody will
// keep up to date for nothing.
func TestEveryKeyTheCodeUsesExistsAndEveryKeyIsUsed(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(here), "..", "..")
	lit := regexp.MustCompile(`i18n\.(?:T|S|M)\((?:[a-zA-Z_.]+, )?"([a-z0-9_.]+)"`)
	used := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range lit.FindAllStringSubmatch(string(src), -1) {
			// "category." + c is a family prefix; its keys are listed below.
			if !strings.HasSuffix(m[1], ".") {
				used[m[1]] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The keys built at run time, by family.
	for _, c := range formats.Categories {
		used["category."+string(c)] = true
	}
	for _, f := range formats.All {
		if i18n.Has("format." + f.ID) {
			used["format."+f.ID] = true
		}
	}
	for _, c := range job.Codes {
		used["error."+string(c)] = true
	}
	var missing, dead []string
	for k := range used {
		if !i18n.Has(k) {
			missing = append(missing, k)
		}
	}
	for _, k := range i18n.Keys() {
		if !used[k] {
			dead = append(dead, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(dead)
	if len(missing) > 0 {
		t.Errorf("the code asks for keys the catalogue does not have: %v", missing)
	}
	if len(dead) > 0 {
		t.Errorf("the catalogue holds keys nothing asks for: %v", dead)
	}
	if len(used) < 100 {
		t.Fatalf("found only %d keys in the code; the scan is not reading it", len(used))
	}
}

func TestNormAndFill(t *testing.T) {
	for in, want := range map[string]string{"de-DE": "de", "tr_TR": "tr", "FR": "fr", "es": "es", "pt-BR": "en", "": "en"} {
		if got := i18n.Norm(in); got != want {
			t.Errorf("Norm(%q) = %q, want %q", in, got, want)
		}
	}
	if got := i18n.S("de", "view.selection_many", "n", 3, "list", "x"); got != "3 Dateien: x" {
		t.Errorf("fill: %q", got)
	}
	// A Text value is taken in the message's own language.
	name := formats.All[0].Names()
	if got := i18n.T("job.converted_one", "format", name); got["tr"] != "PNG biçimine dönüştürüldü" || got["fr"] != "converti en PNG" {
		t.Errorf("T: %v", got)
	}
}
