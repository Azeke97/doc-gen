package documents

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestVerify(t *testing.T) {
	e := newTestEnv(t)
	ctx := t.Context()

	created, _, err := e.documents.Generate(ctx, e.client, request("verify"), "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	doc, err := e.documents.Verify(ctx, created.PublicID)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if doc.PublicID != created.PublicID || doc.TemplateName != "Протокол" || doc.ProjectName != "Test" ||
		!bytes.Equal(doc.FileSha256, created.FileSha256) || doc.FileSize != created.FileSize {
		t.Errorf("doc = %+v", doc)
	}

	for _, id := range []string{strings.Repeat("0", 32), strings.ToUpper(created.PublicID), "", "../" + created.PublicID} {
		if _, err := e.documents.Verify(ctx, id); !errors.Is(err, ErrDocumentNotFound) {
			t.Errorf("Verify(%q): err = %v, want ErrDocumentNotFound", id, err)
		}
	}

	later := *e.documents
	later.now = func() time.Time { return created.ExpiresAt }
	if _, err := later.Verify(ctx, created.PublicID); !errors.Is(err, ErrDocumentExpired) {
		t.Errorf("expired: err = %v, want ErrDocumentExpired", err)
	}
}
