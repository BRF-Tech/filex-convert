package options

import "testing"

func TestAccessors(t *testing.T) {
	o := Options{"quality": float64(70), "crf": "18", "max_height": nil, "pdfa": "on", "bitrate": 128, "pages": " 2-4 "}
	if o.Int("quality") != 70 || o.Int("crf") != 18 || o.Int("max_height") != 0 || o.Int("dpi") != 150 {
		t.Errorf("Int: %d %d %d %d", o.Int("quality"), o.Int("crf"), o.Int("max_height"), o.Int("dpi"))
	}
	if !o.Bool("pdfa") || o.Bool("missing") {
		t.Error("Bool")
	}
	if o.String("bitrate") != "128" || o.String("preset") != "medium" || o.String("pages") != "2-4" {
		t.Errorf("String: %q %q %q", o.String("bitrate"), o.String("preset"), o.String("pages"))
	}
	if (Options{"quality": "x"}).Int("quality") != 85 {
		t.Error("malformed int must answer the default")
	}
}

func TestParsePages(t *testing.T) {
	good := map[string]PageRange{"": {}, "3": {3, 3}, "2-5": {2, 5}, "2-": {2, 0}, " 1 - 2 ": {1, 2}}
	for in, want := range good {
		got, err := ParsePages(in)
		if err != nil || got != want {
			t.Errorf("ParsePages(%q) = %v %v, want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"0", "a", "5-2", "1,3", "-2"} {
		if _, err := ParsePages(bad); err == nil {
			t.Errorf("ParsePages(%q) should fail", bad)
		}
	}
}

func TestValidate(t *testing.T) {
	errs := Options{"quality": 101, "crf": "abc", "preset": "nope", "pages": "x", "dpi": 150}.Validate([]string{Quality, CRF, Preset, Pages, DPI}, "video")
	for _, k := range []string{Quality, CRF, Preset, Pages} {
		if errs[k] == nil || errs[k]["tr"] == "" {
			t.Errorf("%s should be invalid with tr text: %v", k, errs[k])
		}
	}
	if errs[DPI] != nil {
		t.Error("dpi 150 is valid")
	}
	// absent knobs are never errors; an unknown key is ignored
	if errs := (Options{}).Validate([]string{Quality, Preset, "zzz"}, "pdf"); len(errs) != 0 {
		t.Errorf("empty options: %v", errs)
	}
	if errs := (Options{"preset": "ebook"}).Validate([]string{Preset}, "pdf"); len(errs) != 0 {
		t.Errorf("ebook is a pdf preset: %v", errs)
	}
	if errs := (Options{"preset": "ebook"}).Validate([]string{Preset}, "video"); len(errs) == 0 {
		t.Error("ebook is not a video preset")
	}
}

func TestChoicesAndDefaults(t *testing.T) {
	if len(Choices(Preset, "pdf")) != 4 || len(Choices(Preset, "video")) != 5 || len(Choices(Bitrate, "anything")) != 5 {
		t.Error("choice lists")
	}
	if Choices("nope", "") != nil {
		t.Error("unknown knob")
	}
	if DefaultFor(Preset, "pdf") != "ebook" || DefaultFor(Preset, "video") != "medium" || DefaultFor(DPI, "") != 150 || DefaultFor("nope", "") != nil {
		t.Error("defaults")
	}
	for _, k := range Order {
		d, ok := Defs[k]
		if !ok || d.Label["en"] == "" || d.Label["tr"] == "" {
			t.Errorf("knob %s lacks en/tr labels", k)
		}
		if d.Type == "select" {
			for variant, cs := range d.Choices {
				for _, c := range cs {
					if c.Label["en"] == "" || c.Label["tr"] == "" {
						t.Errorf("%s/%s choice %s lacks labels", k, variant, c.Value)
					}
				}
			}
		}
	}
}
