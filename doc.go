// Package typesafe provides an unofficial, dependency-free Go client for the
// TypeSafe v1 API.
//
// Create a [Client] with [NewClient]. By default, NewClient reads the API key
// from TYPESAFE_API_KEY; [WithAPIKey] can supply it directly. A client is safe
// for concurrent use.
//
// Use [Client.SystemOne] to evaluate JSON-compatible state against one or more
// typed questions. [Noul], [Choice], and [Score] construct the supported
// question types. The returned [Response] provides typed answer accessors and
// token usage. Unknown answer types are preserved as [UnknownAnswer], allowing
// applications to handle API additions without losing the original JSON.
//
// Transient failures are retried according to [RetryPolicy]. Errors returned by
// the API can be inspected as [TypeSafeError] with errors.As.
//
// This project is not affiliated with or endorsed by TypeSafe.
//
// TypeSafe API documentation: https://docs.typesafe.ai/
package typesafe
