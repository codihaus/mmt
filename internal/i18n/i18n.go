// Package i18n holds the UI translations. Source strings are English; other
// languages map English text to the translation.
package i18n

import (
	"os"
	"strings"
)

var lang = "en"

var catalogs = map[string]map[string]string{"vi": vi}

// Languages lists the supported language codes.
func Languages() []string { return []string{"en", "vi"} }

// Set selects the UI language; unknown codes fall back to English.
func Set(code string) {
	if _, ok := catalogs[code]; ok {
		lang = code
	} else {
		lang = "en"
	}
}

func Lang() string { return lang }

// Detect picks the language from the config value, then MMT_LANG, then the
// usual locale variables.
func Detect(configured string) string {
	if configured != "" {
		return configured
	}
	for _, v := range []string{"MMT_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		if s := os.Getenv(v); s != "" {
			if strings.HasPrefix(strings.ToLower(s), "vi") {
				return "vi"
			}
			return "en"
		}
	}
	return "en"
}

// Has reports whether the current language has an entry for an English
// string (English itself always does).
func Has(en string) bool {
	c := catalogs[lang]
	if c == nil {
		return true
	}
	_, ok := c[en]
	return ok
}

// T translates an English UI string.
func T(en string) string {
	if c := catalogs[lang]; c != nil {
		if s, ok := c[en]; ok {
			return s
		}
	}
	return en
}
