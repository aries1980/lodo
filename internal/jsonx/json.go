// Package jsonx preserves Lodo's JSON formats and permissive decoding with JSON v2.
package jsonx

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
)

var marshalOptions = json.JoinOptions(
	json.Deterministic(true),
	json.FormatNilSliceAsNull(true),
	json.FormatNilMapAsNull(true),
	jsontext.EscapeForHTML(true),
)

var unmarshalOptions = json.JoinOptions(
	json.MatchCaseInsensitiveNames(true),
	jsontext.AllowDuplicateNames(true),
)

func Marshal(v any) ([]byte, error) {
	return json.Marshal(v, marshalOptions)
}

func MarshalIndent(v any, indent string) ([]byte, error) {
	return json.Marshal(v, marshalOptions, jsontext.WithIndent(indent))
}

func Unmarshal(data []byte, out any) error {
	return json.Unmarshal(data, out, unmarshalOptions)
}
