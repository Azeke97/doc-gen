package htmlengine

import (
	"strings"
	"testing"
)

func TestFooter(t *testing.T) {
	got := string(Footer("0f3c9a1e<b>"))

	for _, want := range []string{"0f3c9a1e&lt;b&gt;", `class="pageNumber"`, `class="totalPages"`, "font:"} {
		if !strings.Contains(got, want) {
			t.Errorf("footer has no %q:\n%s", want, got)
		}
	}
}

func TestWithWatermark(t *testing.T) {
	tests := []struct {
		name string
		page string
		want string
	}{
		{name: "before body end", page: `<html><body><p>x</p></body></html>`, want: `<p>x</p><div style="all: initial;`},
		{name: "upper case tag", page: `<HTML><BODY>x</BODY></HTML>`, want: `x<div style="all: initial;`},
		{name: "no body tag", page: `<p>x</p>`, want: `<p>x</p><div style="all: initial;`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(WithWatermark([]byte(tt.page), "ОБРАЗЕЦ <i>"))

			if !strings.Contains(got, tt.want) {
				t.Errorf("watermark not placed:\n%s", got)
			}
			if !strings.Contains(got, "ОБРАЗЕЦ &lt;i&gt;</div>") {
				t.Errorf("text not escaped:\n%s", got)
			}
		})
	}
}

func TestWithWatermarkKeepsInput(t *testing.T) {
	page := []byte(`<p>x</p>`)
	WithWatermark(page[:len(page):len(page)], "W")

	if string(page) != `<p>x</p>` {
		t.Errorf("input changed: %s", page)
	}
}
