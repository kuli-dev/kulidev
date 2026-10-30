package parse

import (
	"regexp"
	"strings"
)

// maxTerms caps how many distinct body words are indexed per definition.
const maxTerms = 200

var (
	identRe   = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
	camelRe   = regexp.MustCompile(`([a-z0-9])([A-Z])|([A-Z])([A-Z][a-z])`)
	noiseWord = map[string]bool{}
)

func init() {
	for _, w := range strings.Fields(`the and for not nil none null true false self this return func def class
		const let var import from else elif while break continue case switch default type struct interface
		async await new try catch except finally raise throw with yield lambda pass err error string int bool
		float byte rune len make map range append print println printf sprintf errorf any interface`) {
		noiseWord[w] = true
	}
}

// bodyTerms returns the distinct words (identifiers split on camelCase/snake_case,
// plus words in strings and comments) used in a definition, for search recall.
func bodyTerms(body string) string {
	seen := map[string]bool{}
	var out []string
	for _, id := range identRe.FindAllString(body, -1) {
		id = camelRe.ReplaceAllString(id, "$1$3 $2$4")
		for _, w := range strings.FieldsFunc(strings.ToLower(id), func(r rune) bool { return r == '_' || r == ' ' }) {
			if len(w) < 3 || noiseWord[w] || seen[w] {
				continue
			}
			seen[w] = true
			out = append(out, w)
			if len(out) == maxTerms {
				return strings.Join(out, " ")
			}
		}
	}
	return strings.Join(out, " ")
}
