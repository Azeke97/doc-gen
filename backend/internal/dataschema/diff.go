package dataschema

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Виды изменений схемы между версиями.
const (
	KindFieldAdded     = "field_added"
	KindFieldRemoved   = "field_removed"
	KindBecameRequired = "became_required"
	KindTypeChanged    = "type_changed"
	KindEnumNarrowed   = "enum_narrowed"
)

// Change — одно изменение схемы. Breaking — данные, подходившие под старую схему,
// могут не подойти под новую: портал, который их шлёт, начнёт получать ошибки.
type Change struct {
	Path     string `json:"path"` // "lots[].amount"; [] — элемент массива
	Kind     string `json:"kind"`
	Breaking bool   `json:"breaking"`
	Message  string `json:"message"`
}

// Diff сравнивает схемы двух версий по полям: properties, required, items, type, enum.
// $ref и составные ключевые слова (allOf, oneOf, ...) не раскрываются — для них
// изменения внутри не видны. Обе схемы должны быть уже проверены через Compile.
func Diff(oldRaw, newRaw json.RawMessage) ([]Change, error) {
	var oldSchema, newSchema map[string]any
	if err := json.Unmarshal(oldRaw, &oldSchema); err != nil {
		return nil, fmt.Errorf("old schema: %w", err)
	}
	if err := json.Unmarshal(newRaw, &newSchema); err != nil {
		return nil, fmt.Errorf("new schema: %w", err)
	}

	var changes []Change
	diffNode("", oldSchema, newSchema, &changes)

	return changes, nil
}

// HasBreaking сообщает, есть ли среди изменений ломающие.
func HasBreaking(changes []Change) bool {
	return slices.ContainsFunc(changes, func(c Change) bool { return c.Breaking })
}

func diffNode(path string, oldNode, newNode map[string]any, out *[]Change) {
	where := path
	if where == "" {
		where = "(root)"
	}

	// Тип: старые данные подходят, только если новая схема допускает все старые типы.
	oldTypes, newTypes := types(oldNode), types(newNode)
	if len(oldTypes) > 0 && len(newTypes) > 0 && !containsAll(newTypes, oldTypes) {
		*out = append(*out, Change{
			Path: where, Kind: KindTypeChanged, Breaking: true,
			Message: fmt.Sprintf("type changed from %s to %s", strings.Join(oldTypes, "|"), strings.Join(newTypes, "|")),
		})
	}

	// enum: пропавшие значения ломают данные, где они использовались.
	if newEnum, ok := newNode["enum"].([]any); ok {
		oldEnum, hadEnum := oldNode["enum"].([]any)
		var removed []string
		if hadEnum {
			newSet := jsonSet(newEnum)
			for _, v := range oldEnum {
				if s := jsonString(v); !newSet[s] {
					removed = append(removed, s)
				}
			}
		}
		switch {
		case !hadEnum:
			*out = append(*out, Change{Path: where, Kind: KindEnumNarrowed, Breaking: true, Message: "value is now limited to a fixed list (enum)"})
		case len(removed) > 0:
			*out = append(*out, Change{Path: where, Kind: KindEnumNarrowed, Breaking: true, Message: "enum values removed: " + strings.Join(removed, ", ")})
		}
	}

	oldProps, newProps := props(oldNode), props(newNode)
	oldReq, newReq := required(oldNode), required(newNode)
	closed := newNode["additionalProperties"] == false

	for _, name := range sortedKeys(oldProps, newProps) {
		child := join(path, name)
		oldChild, inOld := oldProps[name]
		newChild, inNew := newProps[name]

		switch {
		case !inOld:
			if newReq[name] {
				*out = append(*out, Change{Path: child, Kind: KindFieldAdded, Breaking: true, Message: "new required field"})
			} else {
				*out = append(*out, Change{Path: child, Kind: KindFieldAdded, Message: "new optional field"})
			}
		case !inNew:
			if closed {
				*out = append(*out, Change{Path: child, Kind: KindFieldRemoved, Breaking: true, Message: "field removed and additional properties are not allowed"})
			} else {
				*out = append(*out, Change{Path: child, Kind: KindFieldRemoved, Message: "field removed, the template no longer uses it"})
			}
		default:
			if newReq[name] && !oldReq[name] {
				*out = append(*out, Change{Path: child, Kind: KindBecameRequired, Breaking: true, Message: "field became required"})
			}
			diffNode(child, oldChild, newChild, out)
		}
	}

	// Элементы массива сравниваем как вложенный объект с путём "...[]".
	oldItems, okOld := oldNode["items"].(map[string]any)
	newItems, okNew := newNode["items"].(map[string]any)
	if okOld && okNew {
		diffNode(path+"[]", oldItems, newItems, out)
	}
}

// types возвращает "type" узла в виде списка: в схеме он бывает строкой или массивом.
func types(node map[string]any) []string {
	switch t := node["type"].(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, v := range t {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// containsAll — все типы из want допускаются в have. integer входит в number.
func containsAll(have, want []string) bool {
	for _, w := range want {
		if slices.Contains(have, w) || (w == "integer" && slices.Contains(have, "number")) {
			continue
		}
		return false
	}
	return true
}

func props(node map[string]any) map[string]map[string]any {
	raw, _ := node["properties"].(map[string]any)
	out := make(map[string]map[string]any, len(raw))
	for name, v := range raw {
		if m, ok := v.(map[string]any); ok {
			out[name] = m
		} else {
			out[name] = map[string]any{} // "field": true — любое значение
		}
	}
	return out
}

func required(node map[string]any) map[string]bool {
	list, _ := node["required"].([]any)
	out := make(map[string]bool, len(list))
	for _, v := range list {
		if s, ok := v.(string); ok {
			out[s] = true
		}
	}
	return out
}

func sortedKeys(a, b map[string]map[string]any) []string {
	keys := slices.Collect(maps.Keys(a))
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func jsonSet(values []any) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, v := range values {
		out[jsonString(v)] = true
	}
	return out
}
