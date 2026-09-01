package server

import _ "embed"

// docsHTML is the /docs page.
//
// It is a self-contained document that fetches /openapi.json and renders it
// client-side. Serving a bundled Swagger UI would add a large asset dependency,
// and loading one from a CDN would violate the portfolio's no-external-runtime
// -dependency rule and its own Content-Security-Policy.
//
//go:embed docs.html
var docsHTML []byte
