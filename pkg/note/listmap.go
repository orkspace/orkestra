package note

import (
	"sort"
	"text/template"
)

// listMapNotes registers list/map manipulation helpers.
//
// Usage:
//
//	tmpl.Funcs(note.listMapNotes())
//
// Template examples:
//
//	{{ listHas .spec.regions "us-east-1" }}
//	{{ listGet .spec.regions 0 }}
//	{{ mapGet .metadata.labels "app" }}
//	{{ mapKeys .metadata.labels }}
//	{{ mapValues .metadata.labels }}
func listMapNotes() template.FuncMap {
	return template.FuncMap{
		"listHas":   noteListHas,
		"listGet":   noteListGet,
		"listLen":   noteListLen,
		"mapGet":    noteMapGet,
		"mapKeys":   noteMapKeys,
		"mapValues": noteMapValues,
		"mapPick":   noteMapPick,
		"mapOmit":   noteMapOmit,
	}
}

// noteListHas checks if a list contains a value.
//
//	{{ listHas .spec.regions "us-east-1" }}
func noteListHas(list interface{}, val interface{}) bool {
	items, ok := list.([]interface{})
	if !ok {
		return false
	}
	for _, v := range items {
		if v == val {
			return true
		}
	}
	return false
}

// noteListGet returns list[index] safely.
//
//	{{ listGet .spec.regions 0 }}
func noteListGet(list interface{}, index int) interface{} {
	items, ok := list.([]interface{})
	if !ok || index < 0 || index >= len(items) {
		return nil
	}
	return items[index]
}

// noteListLen returns the length of a list.
//
//	{{ listLen .spec.regions }}
func noteListLen(list interface{}) int {
	items, ok := list.([]interface{})
	if !ok {
		return 0
	}
	return len(items)
}

// noteMapGet returns map[key] safely.
//
//	{{ mapGet .metadata.labels "app" }}
func noteMapGet(m interface{}, key string) interface{} {
	mp, ok := m.(map[string]interface{})
	if !ok {
		return nil
	}
	return mp[key]
}

// noteMapKeys returns all keys of a map.
//
//	{{ mapKeys .metadata.labels }}
func noteMapKeys(m interface{}) []string {
	mp, ok := m.(map[string]interface{})
	if !ok {
		return []string{}
	}
	keys := make([]string, 0, len(mp))
	for k := range mp {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// noteMapValues returns all values of a map.
//
//	{{ mapValues .metadata.labels }}
func noteMapValues(m interface{}) []interface{} {
	mp, ok := m.(map[string]interface{})
	if !ok {
		return []interface{}{}
	}
	values := make([]interface{}, 0, len(mp))
	for _, v := range mp {
		values = append(values, v)
	}
	return values
}

// noteMapPick returns a new map containing only the specified keys.
// Missing keys are silently omitted.
//
//	{{ mapPick .spec "image" "replicas" }}
func noteMapPick(m interface{}, keys ...string) map[string]interface{} {
	mp, ok := m.(map[string]interface{})
	out := make(map[string]interface{}, len(keys))
	if !ok {
		return out
	}
	for _, k := range keys {
		if v, exists := mp[k]; exists {
			out[k] = v
		}
	}
	return out
}

// noteMapOmit returns a new map with the specified keys removed.
//
//	{{ mapOmit .metadata.labels "internal-key" "debug" }}
func noteMapOmit(m interface{}, keys ...string) map[string]interface{} {
	mp, ok := m.(map[string]interface{})
	if !ok {
		return map[string]interface{}{}
	}
	skip := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		skip[k] = struct{}{}
	}
	out := make(map[string]interface{}, len(mp))
	for k, v := range mp {
		if _, omit := skip[k]; !omit {
			out[k] = v
		}
	}
	return out
}
