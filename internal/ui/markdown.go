package ui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	glamouransi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/x/ansi"
)

// emptySGR matches style codes that are reset before anything is printed.
var emptySGR = regexp.MustCompile("(?:\x1b\\[[0-9;]*m)+\x1b\\[0m")

// mdHint is a cheap check for markdown syntax; plain messages skip glamour,
// which is both faster and keeps short chat lines untouched.
var mdHint = regexp.MustCompile("(?m)[*_`#>|\\[]|~~|^\\s*([-+] |\\d+\\. )|:[a-z0-9_+-]+:")

// Rendered markdown is cached per wrap width. Only the most recently used
// widths are kept (two panes plus a resize or two), so resizing does not
// grow the cache without bound.
const mdWidths = 4

type markdown struct {
	style  glamouransi.StyleConfig
	widths []int // most recently used first
	byW    map[int]*mdWidth
}

type mdWidth struct {
	r     *glamour.TermRenderer
	cache map[string][]string
}

func newMarkdown(dark bool) *markdown {
	st := styles.LightStyleConfig
	if dark {
		st = styles.DarkStyleConfig
	}
	zero := uint(0)
	// chat messages sit in a narrow column: drop the document margins
	st.Document.Margin = &zero
	st.Document.BlockPrefix = ""
	st.Document.BlockSuffix = ""
	st.CodeBlock.Margin = &zero
	return &markdown{style: st, byW: map[int]*mdWidth{}}
}

// forWidth returns the renderer and cache for a wrap width, evicting the
// least recently used width when there are too many.
func (md *markdown) forWidth(width int) *mdWidth {
	for i, w := range md.widths {
		if w == width {
			copy(md.widths[1:i+1], md.widths[:i])
			md.widths[0] = width
			return md.byW[width]
		}
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(md.style),
		glamour.WithWordWrap(width),
		glamour.WithEmoji(),
		// Mattermost treats a single newline as a line break
		glamour.WithPreservedNewLines(),
	)
	if err != nil {
		return nil
	}
	e := &mdWidth{r: r, cache: map[string][]string{}}
	md.widths = append([]int{width}, md.widths...)
	md.byW[width] = e
	if len(md.widths) > mdWidths {
		delete(md.byW, md.widths[mdWidths])
		md.widths = md.widths[:mdWidths]
	}
	return e
}

// render returns the styled lines of text, or ok=false when the text has no
// markdown or rendering failed. Results are cached per post revision and width.
func (md *markdown) render(id string, rev int64, text string, width int) (lines []string, ok bool) {
	if !mdHint.MatchString(text) {
		return nil, false
	}
	e := md.forWidth(width)
	if e == nil {
		return nil, false
	}
	key := fmt.Sprintf("%s|%d", id, rev)
	if l, hit := e.cache[key]; hit {
		return l, true
	}
	out, err := e.r.Render(text)
	if err != nil {
		return nil, false
	}
	lines = strings.Split(strings.Trim(out, "\n"), "\n")
	for i, l := range lines {
		// glamour pads every line to the wrap width; trim so right-aligned
		// messages hug the edge
		w := ansi.StringWidth(strings.TrimRight(ansi.Strip(l), " "))
		l = emptySGR.ReplaceAllString(ansi.Truncate(l, w, ""), "")
		if strings.Contains(l, "\x1b[") {
			// truncation can drop the final reset; keep styles from leaking
			// into the frame padding
			l += "\x1b[0m"
		}
		lines[i] = l
	}
	for len(lines) > 0 && ansi.Strip(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if id != "" {
		e.cache[key] = lines
	}
	return lines, true
}

// emoticons mirrors the web client, which turns text smileys into emoji.
var emoticons = map[string]string{
	":)": "🙂", ":-)": "🙂", ":D": "😄", ":-D": "😄", ";)": "😉", ";-)": "😉",
	":(": "🙁", ":-(": "🙁", ":P": "😛", ":p": "😛", ":-P": "😛", ":o": "😮", ":O": "😮",
	":'(": "😢", ":|": "😐", "<3": "❤️", "</3": "💔", "B-)": "😎", ":/": "😕",
}

// replaceEmoticons swaps smileys that stand alone as a word; "http://x" and
// code such as "a:)b" are left alone.
func replaceEmoticons(s string) string {
	if !strings.ContainsAny(s, ":;<B") {
		return s
	}
	lines := strings.Split(s, "\n")
	inCode := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			continue
		}
		words := strings.Split(line, " ")
		for j, w := range words {
			if e, ok := emoticons[w]; ok {
				words[j] = e
			}
		}
		lines[i] = strings.Join(words, " ")
	}
	return strings.Join(lines, "\n")
}
