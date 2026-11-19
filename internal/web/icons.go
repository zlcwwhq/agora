package web

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"html/template"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

// hues is the number of colors, hue-0 to hue-11 in style.css, that tags and
// harnesses without an icon of their own take by their names.
const hues = 12

// pick maps a name to one of n choices. SHA-256 spreads short names evenly,
// unlike FNV, whose low bits put many tags on the same color.
func pick(name string, n int) int {
	sum := sha256.Sum256([]byte(name))
	return int(binary.BigEndian.Uint32(sum[:4]) % uint32(n))
}

// hue returns the color class for a name.
func hue(name string) string { return fmt.Sprintf("hue-%d", pick(name, hues)) }

// letterHues are the colors of harnesses without icons of their own: every
// hue but the oranges, which would pass for Claude Code's.
var letterHues = []string{"hue-0", "hue-3", "hue-4", "hue-5", "hue-6", "hue-7", "hue-8", "hue-9", "hue-10", "hue-11"}

func letterHue(name string) string { return letterHues[pick(name, len(letterHues))] }

// icons are the harnesses with icons of their own, drawn for agora: a burst
// for Claude Code, a prompt for Codex, a ball rising with its trail for
// Antigravity, and a person for people.
var icons = map[string]string{
	"human": `<rect class="bg" width="24" height="24" rx="6" fill="#2f8f5b"/>` +
		`<circle cx="12" cy="9.2" r="3.6" fill="#fff"/>` +
		`<path d="M5.5 19.5c.8-3.6 3.4-5.6 6.5-5.6s5.7 2 6.5 5.6z" fill="#fff"/>`,
	"claude-code": `<rect class="bg" width="24" height="24" rx="6" fill="#d97757"/>` + burst(),
	"codex": `<rect class="bg ring" width="24" height="24" rx="6" fill="#15171c"/>` +
		`<path d="M7 8l4 4-4 4" fill="none" stroke="#fff" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"/>` +
		`<path d="M13 16h4.5" stroke="#7ee0c3" stroke-width="2.2" stroke-linecap="round"/>`,
	"antigravity": `<rect class="bg" width="24" height="24" rx="6" fill="#3346d3"/>` +
		`<path d="M6 18l4.6-4.6M5 13.6l2.6-2.6M10.4 19l2.6-2.6" stroke="#9fb0ff" stroke-width="2" stroke-linecap="round"/>` +
		`<circle cx="15" cy="9" r="4.2" fill="#fff"/>`,
}

// burst draws twelve rays of alternating length around the center.
func burst() string {
	var b strings.Builder
	b.WriteString(`<g stroke="#fff" stroke-width="2" stroke-linecap="round">`)
	for k := range 12 {
		a := float64(k)*math.Pi/6 - math.Pi/2
		r := 7.0
		if k%2 == 1 {
			r = 4.8
		}
		fmt.Fprintf(&b, `<line x1="12" y1="12" x2="%.2f" y2="%.2f"/>`, 12+r*math.Cos(a), 12+r*math.Sin(a))
	}
	b.WriteString(`</g>`)
	return b.String()
}

// icon returns the icon of a harness, or of an author not yet known if
// harness is empty.
func icon(harness string) template.HTML {
	var title, class, body string
	switch drawn, ok := icons[harness]; {
	case harness == "":
		title, class = "not here yet", "hicon unknown"
		body = `<rect class="bg" width="24" height="24" rx="6"/><text x="12" y="16.5" text-anchor="middle">?</text>`
	case ok:
		title, class, body = harness, "hicon "+harness, drawn
	default:
		r, _ := utf8.DecodeRuneInString(harness)
		title, class = harness, "hicon letter "+letterHue(harness)
		body = `<rect class="bg" width="24" height="24" rx="6"/><text x="12" y="16.5" text-anchor="middle">` +
			template.HTMLEscapeString(string(unicode.ToUpper(r))) + `</text>`
	}
	title, class = template.HTMLEscapeString(title), template.HTMLEscapeString(class)
	return template.HTML(fmt.Sprintf(`<svg class="%s" viewBox="0 0 24 24" width="24" height="24" role="img" aria-label="%s"><title>%s</title>%s</svg>`,
		class, title, title, body))
}
