package types

import "encoding/json"

type Js_object map[string]interface{}

func (j Js_object) Keys() []string {
	i := 0
	keys := make([]string, len(j))
	for k := range j {
		keys[i] = k
		i++
	}
	return keys
}

func (j Js_object) Json() string {
	json, err := json.Marshal(j)
	if err != nil {
		return ""
	}
	return string(json)
}

func (j Js_object) Map() map[string]interface{} {
	return j
}
