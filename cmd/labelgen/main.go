// Command labelgen renders the label rule language from the code that runs it,
// the rule-language arm of the generation pipeline. Two artifacts, because the
// rule language has two closed halves and they are declared in different places:
//
//   - docs/src/generated/label.json, the FUNCTIONS a rule may name, declared once
//     in internal/label so the FuncMap, the published names, the AST allowlist and
//     the docs table are four readers of one list rather than four transcriptions
//     (#701). Each example is executed through the engine on the way in.
//   - docs/src/generated/labeldata.json, the DATA a rule may read, declared once
//     per entity kind in internal/storage beside the accessors that build the map
//     (#729), so the taught set and the reachable set cannot disagree.
//
// Both committed files are reviewed like any generated code; the drift tests in
// internal/label and internal/storage fail when either goes stale.
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/hyperscaleav/omniglass/internal/label"
	"github.com/hyperscaleav/omniglass/internal/notify"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

const (
	funcDst = "docs/src/generated/label.json"
	dataDst = "docs/src/generated/labeldata.json"
	// The alarm fact map a notification template reads (#618), declared in
	// internal/storage beside the accessor that builds it, rendered through
	// the same sandbox as a label rule.
	notifyDst = "docs/src/generated/notificationfacts.json"
	// The webhook envelope's JSON Schema, generated from its Go struct, so the
	// documented payload is the sent payload (#618).
	envelopeDst = "docs/src/generated/notification-envelope.json"
)

func main() {
	funcs, err := label.FactsJSON()
	if err != nil {
		log.Fatalf("labelgen: %v", err)
	}
	write(funcDst, funcs)
	log.Printf("wrote the rule language to %s", funcDst)

	data, err := storage.LabelDataFactsJSON()
	if err != nil {
		log.Fatalf("labelgen: %v", err)
	}
	write(dataDst, data)
	log.Printf("wrote the rule data map to %s", dataDst)

	facts, err := storage.NotificationFactsJSON()
	if err != nil {
		log.Fatalf("labelgen: %v", err)
	}
	write(notifyDst, facts)
	log.Printf("wrote the notification fact map to %s", notifyDst)

	envelope, err := notify.EnvelopeSchemaJSON()
	if err != nil {
		log.Fatalf("labelgen: %v", err)
	}
	write(envelopeDst, envelope)
	log.Printf("wrote the webhook envelope schema to %s", envelopeDst)
}

func write(dst string, body []byte) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		log.Fatalf("labelgen: mkdir: %v", err)
	}
	if err := os.WriteFile(dst, body, 0o644); err != nil {
		log.Fatalf("labelgen: write %s: %v", dst, err)
	}
}
