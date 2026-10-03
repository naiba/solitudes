package pagination

import "testing"

func TestParsePageBounds(t *testing.T) {
	for _, raw := range []string{"-1", "0", "1001", "99999999999999999999", "1.5", "abc"} {
		if _, err := Parse(raw); err == nil {
			t.Errorf("accepted page %q", raw)
		}
	}
	for _, raw := range []string{"", "1", "2", "1000"} {
		if _, err := Parse(raw); err != nil {
			t.Errorf("rejected page %q: %v", raw, err)
		}
	}
}

func TestPagingRejectsUnsafeBoundsBeforeQuery(t *testing.T) {
	for _, input := range []Param{{Page: -1}, {Page: MaxPage + 1}, {Limit: -1}, {Limit: 101}} {
		if _, err := Paging(&input, nil); err == nil {
			t.Errorf("accepted %+v", input)
		}
	}
}
