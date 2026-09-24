package formats

import (
	"regexp"
	"strings"
	"testing"

	"github.com/brf-tech/filex-convert/internal/i18n"
)

func TestCatalogueConsistency(t *testing.T) {
	ids := map[string]bool{}
	exts := map[string]string{}
	validCat := map[Category]bool{}
	for _, c := range Categories {
		validCat[c] = true
		for _, lang := range i18n.Langs {
			if CategoryLabel(c)[lang] == "" {
				t.Errorf("category %s has no %s label", c, lang)
			}
		}
	}
	for _, f := range All {
		if f.ID == "" || strings.ToLower(f.ID) != f.ID {
			t.Errorf("bad id %q", f.ID)
		}
		if ids[f.ID] {
			t.Errorf("duplicate id %q", f.ID)
		}
		ids[f.ID] = true
		if len(f.Ext) == 0 {
			t.Errorf("%s: no extensions", f.ID)
		}
		for _, e := range f.Ext {
			if e != strings.ToLower(e) || strings.HasPrefix(e, ".") {
				t.Errorf("%s: extension %q must be lower-case without a dot", f.ID, e)
			}
			if prev, dup := exts[e]; dup {
				t.Errorf("extension %q claimed by %s and %s", e, prev, f.ID)
			}
			exts[e] = f.ID
		}
		if f.Mime == "" || !strings.Contains(f.Mime, "/") {
			t.Errorf("%s: bad mime %q", f.ID, f.Mime)
		}
		if !validCat[f.Category] {
			t.Errorf("%s: unknown category %q", f.ID, f.Category)
		}
		if f.Label == "" {
			t.Errorf("%s: no label", f.ID)
		}
	}
	if len(All) < 35 {
		t.Errorf("catalogue has %d formats, expected at least 35", len(All))
	}
}

func TestDetect(t *testing.T) {
	cases := []struct {
		name, id, stem string
		ok             bool
	}{
		{"photo.JPG", "jpg", "photo", true},
		{"photo.jpeg", "jpg", "photo", true},
		{"a/b/c/site.tar.gz", "tgz", "site", true},
		{"backup.tgz", "tgz", "backup", true},
		{"backup.tar.zst", "tzst", "backup", true},
		{"notes.markdown", "md", "notes", true},
		{"deck.pptx", "pptx", "deck", true},
		{"archive.xyz", "", "archive", false},
		{"noext", "", "noext", false},
		{".hidden", "", ".hidden", false},
		{"trailing.", "", "trailing.", false},
		{`C:\dir\clip.MOV`, "mov", "clip", true},
		{"my.report.v2.pdf", "pdf", "my.report.v2", true},
	}
	for _, c := range cases {
		f, stem, ok := Detect(c.name)
		if ok != c.ok {
			t.Errorf("%s: ok=%v want %v", c.name, ok, c.ok)
			continue
		}
		if stem != c.stem {
			t.Errorf("%s: stem=%q want %q", c.name, stem, c.stem)
		}
		if ok && f.ID != c.id {
			t.Errorf("%s: id=%q want %q", c.name, f.ID, c.id)
		}
	}
}

func TestLookups(t *testing.T) {
	if f, ok := ByExt(".TIF"); !ok || f.ID != "tiff" {
		t.Errorf("ByExt(.TIF) = %v %v", f, ok)
	}
	if _, ok := ByID("nope"); ok {
		t.Error("ByID(nope) should fail")
	}
	if got := len(InCategory(Archive)); got != 7 {
		t.Errorf("archive category has %d formats, want 7", got)
	}
	if len(IDs()) != len(All) {
		t.Error("IDs() length mismatch")
	}
}

// A label is a NAME ("PNG", "Word") or a DESCRIPTION ("Plain text"). A name
// reads the same in every language; a description is English, and on
// another language's wizard it is a half-translated button — the v0.43.0
// sweep found "Plain text (.txt)" and "Icon (.ico)" on the Turkish one.
// Every label carrying one of these English words must be said in every
// language the converter ships, and no translation may carry the English
// word again.
func TestEveryDescriptiveLabelIsTranslated(t *testing.T) {
	english := []string{"Plain", "text", "Text", "Icon", "Animated", "Rich", "Spreadsheet", "Presentation"}
	// Words English shares with a language are that language's own: German
	// writes "Text" too ("Reiner Text").
	shared := map[string]map[string]bool{"de": {"Text": true, "text": true}}
	word := func(s, w string) bool {
		return regexp.MustCompile(`` + regexp.QuoteMeta(w) + ``).MatchString(s)
	}
	for _, f := range All {
		descriptive := false
		for _, w := range english {
			// "Windows Media Audio", "Flash Video", "DirectDraw Surface" are
			// product names; the words above are not in any of them.
			if strings.Contains(f.Label, w) {
				descriptive = true
			}
		}
		key := "format." + f.ID
		if !descriptive {
			if i18n.Has(key) {
				t.Errorf("%s: %q is a name, the same in every language; a %s key is noise", f.ID, f.Label, key)
			}
			continue
		}
		if !i18n.Has(key) {
			t.Errorf("%s: %q describes the format in English and has no %s key", f.ID, f.Label, key)
			continue
		}
		if f.Name("en") != f.Label {
			t.Errorf("%s: the English label must stay %q, got %q", f.ID, f.Label, f.Name("en"))
		}
		for _, lang := range i18n.Langs[1:] {
			got := f.Name(lang)
			if got == f.Label {
				t.Errorf("%s: the %s label is the English one", f.ID, lang)
			}
			for _, w := range english {
				if word(got, w) && !shared[lang][w] {
					t.Errorf("%s: the %s label %q still carries %q", f.ID, lang, got, w)
				}
			}
		}
	}
}
