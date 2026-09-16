// Package typesafe is an unofficial community Go SDK for the TypeSafe API.
package typesafe

// Entry is a JSON-compatible value accepted by the TypeSafe API. It may be a
// string, number, boolean, nil, slice, map, or any value encoding/json can
// marshal as JSON.
type Entry = any
