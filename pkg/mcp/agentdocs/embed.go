// Package agentdocs embeds the documents an assistant reads before calling the
// Activity MCP tools over HTTP: a knowledge document describing the Activity
// model, and skills (runbooks) giving step-by-step procedures.
//
// The files live under pkg/ so the container build, which copies only cmd/,
// pkg/ and internal/, includes them.
package agentdocs

import "embed"

const (
	// KnowledgeFile is the path of the knowledge document within FS.
	KnowledgeFile = "llms-full.txt"

	// SkillsDir is the directory of skill documents within FS. Each file is
	// served at /runbooks/<name>.md.
	SkillsDir = "skills"
)

// FS holds the knowledge document and every skill.
//
//go:embed llms-full.txt skills/*.md
var FS embed.FS
