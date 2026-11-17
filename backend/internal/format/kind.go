package format

import (
	"encoding/json"
	"fmt"
)

// kind называет тип JSON-значения по-русски — для сообщений об ошибках в данных.
func kind(v any) string {
	switch v.(type) {
	case bool:
		return "true/false"
	case string:
		return "строка"
	case json.Number, int, int64, float64:
		return "число"
	case []any:
		return "список"
	case map[string]any:
		return "объект"
	}
	return fmt.Sprintf("%T", v)
}
