// Package waformat converts rich text from other platforms to WhatsApp formatting.
package waformat

import "regexp"

var (
	reBold   = regexp.MustCompile(`\*\*([^*\n]+?)\*\*`)
	reItalic = regexp.MustCompile(`(^|[^*\w])\*([^*\n]+?)\*([^*\w]|$)`)
	reStrike = regexp.MustCompile(`~~([^~\n]+?)~~`)
)

// FromMarkdown converts Markdown (as produced by Chatwoot and Typebot) to
// WhatsApp formatting: **bold** -> *bold*, *italic* -> _italic_, ~~x~~ -> ~x~.
// _italic_ is already valid WhatsApp syntax and is kept as is.
func FromMarkdown(s string) string {
	// Italic first, while bold is still double-starred and can't match it.
	s = reItalic.ReplaceAllString(s, "${1}_${2}_${3}")
	s = reBold.ReplaceAllString(s, "*${1}*")
	s = reStrike.ReplaceAllString(s, "~${1}~")
	return s
}
