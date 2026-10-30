// Package skills contains the skill definitions distributed with the CLI.
package skills

import "embed"

// Files holds the bundled skill definitions.
//
//go:embed prop-review/SKILL.md prop-review-ingest/SKILL.md
var Files embed.FS
