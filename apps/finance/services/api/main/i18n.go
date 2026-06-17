package main

import (
	"embed"
	"log/slog"
	"net/http"
	"strings"

	"github.com/BurntSushi/toml"
)

//go:embed locales/*.toml
var localeFS embed.FS

const defaultLang = "en"

var supportedLangs = map[string]bool{"en": true, "pt": true}

// catalogue holds the flattened key→string map for one language.
type catalogue map[string]string

var catalogues = map[string]catalogue{}

func init() {
	for lang := range supportedLangs {
		cat, err := loadCatalogue(lang)
		if err != nil {
			slog.Error("i18n: failed to load locale", "lang", lang, "err", err)
			continue
		}
		catalogues[lang] = cat
	}
}

func loadCatalogue(lang string) (catalogue, error) {
	data, err := localeFS.ReadFile("locales/" + lang + ".toml")
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	cat := make(catalogue)
	flattenTOML("", raw, cat)
	return cat, nil
}

// flattenTOML recursively flattens nested TOML tables into dot-notation keys.
func flattenTOML(prefix string, node map[string]any, out catalogue) {
	for k, v := range node {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch val := v.(type) {
		case string:
			out[key] = val
		case map[string]any:
			flattenTOML(key, val, out)
		}
	}
}

// Translator wraps a locale lookup and exposes a Get method callable from
// Go templates as {{.T.Get "key"}}.
type Translator struct {
	lang string
	cat  catalogue
	en   catalogue
}

func (tr *Translator) Get(key string) string {
	if s, ok := tr.cat[key]; ok {
		return s
	}
	if s, ok := tr.en[key]; ok {
		return s
	}
	return key
}

// Lang returns the active language code for use in templates (e.g. {{.T.Lang}}).
func (tr *Translator) Lang() string {
	return tr.lang
}

// newT returns a Translator for the given language.
func newT(lang string) *Translator {
	cat, ok := catalogues[lang]
	if !ok {
		lang = defaultLang
		cat = catalogues[defaultLang]
	}
	return &Translator{lang: lang, cat: cat, en: catalogues[defaultLang]}
}

// detectLang reads the lang from a cookie, falling back to Accept-Language,
// then to the default. Only returns a supported language code.
func detectLang(r *http.Request) string {
	if c, err := r.Cookie("lang"); err == nil {
		if supportedLangs[c.Value] {
			return c.Value
		}
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		code := strings.ToLower(strings.SplitN(tag, "-", 2)[0])
		if supportedLangs[code] {
			return code
		}
	}
	return defaultLang
}

// setLangCookie writes the lang preference cookie.
func setLangCookie(w http.ResponseWriter, lang string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     "lang",
		Value:    lang,
		Path:     "/",
		HttpOnly: false, // JS may read it for future enhancement
		Secure:   secure,
		MaxAge:   365 * 24 * 3600,
		SameSite: http.SameSiteStrictMode,
	})
}
