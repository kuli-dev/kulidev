package docs

import (
	"strings"
	"testing"
)

func TestResolveCitationForms(t *testing.T) {
	f := NewFacts()
	f.Cite("a.go", 10, 50) // E1
	f.Cite("b.go", 1, 5)   // E2
	md := "One [E1]. Two [E1 L12-14]. Three [E1 L15, L20]. Four [E1, E2]. " +
		"Bad range [E1 L99]. Unknown [E7]. Weird [E1 lines 3 to 4]."
	out, st := f.Resolve(md, "")
	for _, want := range []string{"One (a.go:10-50)", "Two (a.go:12-14)", "Three (a.go:15, a.go:20)", "Four (a.go:10-50, b.go:1-5)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, "[E") {
		t.Errorf("raw tag leaked: %q", out)
	}
	if st.Valid != 6 || st.Removed != 3 {
		t.Errorf("stats = %+v", st)
	}
}
