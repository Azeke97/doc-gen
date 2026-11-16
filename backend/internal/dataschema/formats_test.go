package dataschema

import (
	"encoding/json"
	"testing"
)

// Номера синтетические: контрольная цифра посчитана по алгоритму, реальных людей и организаций за ними нет.
func TestKzFormatsInSchema(t *testing.T) {
	s := mustCompile(t, `{"type": "object", "properties": {
		"bin": {"type": "string", "format": "kz-bin"},
		"iin": {"type": "string", "format": "kz-iin"}
	}}`)

	errs, err := s.Validate(json.RawMessage(`{"bin": "900101300017", "iin": "900101300016"}`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(errs) != 1 || errs[0].Field != "/iin" {
		t.Errorf("errors = %+v, want one on /iin", errs)
	}
}
