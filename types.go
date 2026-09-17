package typesafe

// Entry is a JSON-compatible value accepted in TypeSafe state and question
// fields. Top-level state and instructions must be strings, objects, or arrays;
// nested values may also contain finite numbers, booleans, and nil.
type Entry = any
