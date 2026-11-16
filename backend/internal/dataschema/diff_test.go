package dataschema

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestDiff(t *testing.T) {
	tests := []struct {
		name string
		old  string
		new  string
		want []Change
	}{
		{
			name: "same",
			old:  protocolSchema,
			new:  protocolSchema,
		},
		{
			name: "optional field added",
			old:  `{"type": "object", "properties": {"a": {"type": "string"}}}`,
			new:  `{"type": "object", "properties": {"a": {"type": "string"}, "b": {"type": "string"}}}`,
			want: []Change{{Path: "b", Kind: KindFieldAdded, Message: "new optional field"}},
		},
		{
			name: "required field added",
			old:  `{"type": "object", "properties": {}}`,
			new:  `{"type": "object", "required": ["b"], "properties": {"b": {"type": "string"}}}`,
			want: []Change{{Path: "b", Kind: KindFieldAdded, Breaking: true, Message: "new required field"}},
		},
		{
			name: "field became required",
			old:  `{"type": "object", "properties": {"a": {"type": "string"}}}`,
			new:  `{"type": "object", "required": ["a"], "properties": {"a": {"type": "string"}}}`,
			want: []Change{{Path: "a", Kind: KindBecameRequired, Breaking: true, Message: "field became required"}},
		},
		{
			name: "field removed",
			old:  `{"type": "object", "properties": {"a": {"type": "string"}}}`,
			new:  `{"type": "object", "properties": {}}`,
			want: []Change{{Path: "a", Kind: KindFieldRemoved, Message: "field removed, the template no longer uses it"}},
		},
		{
			name: "field removed from closed object",
			old:  `{"type": "object", "properties": {"a": {"type": "string"}}}`,
			new:  `{"type": "object", "additionalProperties": false, "properties": {}}`,
			want: []Change{
				{Path: "(root)", Kind: KindClosed, Breaking: true, Message: "additional properties are no longer allowed"},
				{Path: "a", Kind: KindFieldRemoved, Breaking: true, Message: "field removed and additional properties are not allowed"},
			},
		},
		{
			name: "required without properties",
			old:  `{"type": "object"}`,
			new:  `{"type": "object", "required": ["number"]}`,
			want: []Change{{Path: "number", Kind: KindFieldAdded, Breaking: true, Message: "new required field"}},
		},
		{
			name: "constraints tightened",
			old:  `{"type": "object", "properties": {"s": {"type": "string", "maxLength": 100}, "n": {"type": "number", "minimum": 0}}}`,
			new:  `{"type": "object", "properties": {"s": {"type": "string", "maxLength": 50, "pattern": "^[0-9]+$"}, "n": {"type": "number", "minimum": 1}}}`,
			want: []Change{
				{Path: "n", Kind: KindConstraint, Breaking: true, Message: "minimum changed from 0 to 1"},
				{Path: "s", Kind: KindConstraint, Breaking: true, Message: "maxLength changed from 100 to 50"},
				{Path: "s", Kind: KindConstraint, Breaking: true, Message: `pattern added: "^[0-9]+$"`},
			},
		},
		{
			name: "constraints relaxed is fine",
			old:  `{"type": "object", "properties": {"s": {"type": "string", "maxLength": 50, "minLength": 5, "format": "kz-bin"}}}`,
			new:  `{"type": "object", "properties": {"s": {"type": "string", "maxLength": 100, "minLength": 1}}}`,
		},
		{
			name: "nested type changed in array items",
			old:  protocolSchema,
			new: `{"type": "object", "required": ["number", "lots"], "properties": {
				"number": {"type": "string"}, "date": {"type": "string"},
				"lots": {"type": "array", "items": {"type": "object", "required": ["name", "amount"],
					"properties": {"name": {"type": "string"}, "amount": {"type": "string"}}}}}}`,
			want: []Change{{Path: "lots[].amount", Kind: KindTypeChanged, Breaking: true, Message: "type changed from number to string"}},
		},
		{
			name: "type widened is fine",
			old:  `{"type": "object", "properties": {"a": {"type": "integer"}}}`,
			new:  `{"type": "object", "properties": {"a": {"type": ["number", "null"]}}}`,
		},
		{
			name: "enum narrowed",
			old:  `{"type": "object", "properties": {"s": {"enum": ["open", "closed", "cancelled"]}}}`,
			new:  `{"type": "object", "properties": {"s": {"enum": ["open", "closed"]}}}`,
			want: []Change{{Path: "s", Kind: KindEnumNarrowed, Breaking: true, Message: `enum values removed: "cancelled"`}},
		},
		{
			name: "enum introduced",
			old:  `{"type": "object", "properties": {"s": {"type": "string"}}}`,
			new:  `{"type": "object", "properties": {"s": {"type": "string", "enum": ["open"]}}}`,
			want: []Change{{Path: "s", Kind: KindEnumNarrowed, Breaking: true, Message: "value is now limited to a fixed list (enum)"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Diff(json.RawMessage(tt.old), json.RawMessage(tt.new))
			if err != nil {
				t.Fatalf("Diff: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("changes = %+v\nwant %+v", got, tt.want)
			}
			if HasBreaking(got) != slices.ContainsFunc(tt.want, func(c Change) bool { return c.Breaking }) {
				t.Errorf("HasBreaking = %v", HasBreaking(got))
			}
		})
	}
}
