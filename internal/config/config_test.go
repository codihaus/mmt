package config

import "testing"

func TestNormalizeURL(t *testing.T) {
	cases := map[string]string{
		"":                            "",
		"chat.example.com":            "https://chat.example.com",
		" https://chat.example.com/ ": "https://chat.example.com",
		"http://localhost:8065//":     "http://localhost:8065",
	}
	for in, want := range cases {
		if got := NormalizeURL(in); got != want {
			t.Errorf("NormalizeURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEnvServerIsNotSaved(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MMT_URL", "chat.example.com")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.ServerURL != "https://chat.example.com" {
		t.Fatalf("server = %q", c.ServerURL)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MMT_URL", "")
	c, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.ServerURL != "" {
		t.Errorf("MMT_URL leaked into the config file: %q", c.ServerURL)
	}
}
