# TypeSafe Go SDK
An **unofficial community SDK** for the [TypeSafe](https://www.typesafe.ai/) v1 API. It provides a small, dependency-free Go client for System One and model discovery.

> This project is not affiliated with or endorsed by TypeSafe.

## Install

```sh
go get github.com/zhirschtritt/typesafe-go
```

Requires Go 1.23 or later.

## Quickstart

Set `TYPESAFE_API_KEY`, then ask related questions in one System One request:

```go
package main

import (
	"context"
	"fmt"
	"log"

	typesafe "github.com/zhirschtritt/typesafe-go"
)

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	response, err := client.SystemOne(context.Background(),
		map[string]any{
			"draft": "Ship the migration on Friday.",
			"author": "Avery",
		},
		map[string]typesafe.Question{
			"safe": typesafe.Noul("Is the draft safe to send?", nil),
			"audience": typesafe.Choice("Which audience should receive the draft?", map[string]typesafe.Entry{
				"engineering": "Technical stakeholders",
				"customers":   "External customers",
			}),
			"feasibility": typesafe.Score("How feasible is shipping the migration on Friday?",
				"Not feasible", "At risk", "Feasible",
			),
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	safe, _ := response.NoulAnswer("safe")
	audience, _ := response.ChoiceAnswer("audience")
	feasibility, _ := response.ScoreAnswer("feasibility")
	fmt.Printf("safe=%.2f audience=%s feasibility=%.2f request_id=%s\n",
		safe.Noul, audience.Choice, feasibility.Score, response.RequestID)
}
```

`SystemOne` batches all questions over the same state. Keep shared state and instructions focused. Put common context in `state`; reserve each question for its distinct judgment.

## Configuration

`NewClient()` reads these environment variables:

| Variable | Purpose |
| --- | --- |
| `TYPESAFE_API_KEY` | API key (required unless supplied with a client option) |
| `TYPESAFE_BASE_URL` | API base URL override |
| `TYPESAFE_DEFAULT_MODEL` | Default model override |

Use client options to configure a key, base URL, default model, retry behavior, HTTP transport, and maximum response size. Request options can override request-scoped settings such as the model without mutating the client.

Calls are context-first. Canceling the supplied `context.Context` stops a request and prevents retries.

## Retries and errors

Transient failures are retried according to the configured retry policy. The client honors server `Retry-After` responses and never retries a canceled context. Every successful response exposes `RequestID`; typed HTTP errors include the response status, headers, request ID, and bounded response body for diagnostics.

Validate questions before sending them: empty or invalid question definitions, invalid state, and invalid client or request options return an error locally.

## Releases

Releases use [Semantic Versioning](https://semver.org/) and are prepared from [Conventional Commits](https://www.conventionalcommits.org/):

- `fix:` produces a patch release.
- `feat:` produces a minor release.
- A `!` after the type or scope, such as `feat!:` or `feat(api)!:`, produces a major release. A `BREAKING CHANGE:` footer has the same effect.
- Other commit types do not produce a release by themselves.

See [CONTRIBUTING.md](CONTRIBUTING.md) for local setup, validation commands, and the Conventional Commit format used by the release workflow.

After CI passes on `main`, Release Please opens or updates a release pull request. Merging that pull request creates the `vX.Y.Z` tag and GitHub Release. Release notes are derived from the commits; the repository does not maintain a changelog file. The release pull request also updates `Version`, which is sent in SDK request headers.

## Forward compatibility

The decoder accepts additive JSON fields. If the service returns an answer type newer than this SDK, it is preserved as `*typesafe.UnknownAnswer` with its raw JSON rather than discarded. Handle it explicitly when consuming evolving API responses.

## Future improvements

- When Go 1.27 is an acceptable minimum version, use generic concrete methods for `Client.SystemOne` and generic question constructors. This will preserve each call's concrete state and instruction types until JSON encoding while allowing one client to accept different types across calls. JSON compatibility will still require runtime validation because an `any` constraint cannot exclude values unsupported by `encoding/json`.

## References

- [TypeSafe documentation](https://docs.typesafe.ai/)
- [Official JavaScript SDK](https://github.com/typesafe-ai/typesafe-sdk-js)
- [Official Python SDK](https://github.com/typesafe-ai/typesafe-sdk-python)
- [TypeSafe OpenAPI specification](https://api.typesafe.ai/openapi.json)

Security issues should be reported privately as described in [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
