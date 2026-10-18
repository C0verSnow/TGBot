package allowlist

import _ "embed"

// Whitelist is bundled into each function; it never depends on runtime files.
//
//go:embed idlist.md
var Whitelist string
