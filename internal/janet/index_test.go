package janet

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestWorkspaceReferencesRenameAndShadowing(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "model.janet", `(defn target
  "Target documentation."
  [value]
  value)

(defn locally-shadowed [target]
  (target 0))

(let [target (fn [value] value)]
  (target 1))

(target 2)
`)
	writeFixture(t, root, "consumer.janet", `(import ./model)
(model/target 3)
`)
	writeFixture(t, root, "alias.janet", `(import ./model :as m)
(m/target 4)
`)

	index, err := BuildIndex(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	model := index.Documents[PathToURI(filepath.Join(root, "model.janet"))]
	declarationOffset := strings.Index(model.Source, "target")
	target, ok := index.OccurrenceAt(model.URI, declarationOffset)
	if !ok {
		t.Fatal("target declaration did not resolve")
	}

	references := index.References(target.ID, true)
	if got := occurrenceNames(references); strings.Join(got, ",") != "m/target,model/target,target,target" {
		t.Fatalf("references = %v", got)
	}
	if got := len(index.References(target.ID, false)); got != 3 {
		t.Fatalf("references without declaration = %d, want 3", got)
	}

	changes, err := index.Rename(target.ID, "renamed")
	if err != nil {
		t.Fatal(err)
	}
	if got := replacementTexts(changes); strings.Join(got, ",") != "m/renamed,model/renamed,renamed,renamed" {
		t.Fatalf("rename replacements = %v", got)
	}

	parameterUse := strings.Index(model.Source, "(target 0)") + 1
	local, ok := index.OccurrenceAt(model.URI, parameterUse)
	if !ok || local.ID == target.ID {
		t.Fatal("shadowing parameter resolved to the top-level definition")
	}
	if got := len(index.References(local.ID, true)); got != 2 {
		t.Fatalf("local parameter references = %d, want declaration and use", got)
	}
	localDefinition, ok := index.DefinitionFor(local)
	if !ok || localDefinition.Start == target.Start {
		t.Fatal("local parameter definition was not indexed independently")
	}
}

func TestImportPrefixCompletionAndSignature(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "model.janet", `(defn target "Docs." [value]
  value)
`)
	writeFixture(t, root, "consumer.janet", `(import ./model :prefix "api/")
(api/target 3)
`)
	index, err := BuildIndex(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	consumer := index.Documents[PathToURI(filepath.Join(root, "consumer.janet"))]
	callOffset := strings.Index(consumer.Source, "api/target")
	occurrence, ok := index.OccurrenceAt(consumer.URI, callOffset)
	if !ok {
		t.Fatal("prefixed import call did not resolve")
	}
	definition, ok := index.DefinitionFor(occurrence)
	if !ok || definition.Name != "target" || definition.Documentation != "Docs." {
		t.Fatalf("definition = %#v", definition)
	}
	signature, ok := index.SignatureAt(consumer.URI, callOffset+len("api/target"))
	if !ok || !strings.HasPrefix(signature.Signature, "(defn target") {
		t.Fatalf("signature = %#v", signature)
	}
	items := index.Completions(consumer.URI, callOffset)
	if !hasCompletion(items, "api/target") {
		t.Fatalf("completion missing api/target: %#v", items)
	}
}

func TestOpenDocumentOverlayWinsOverDisk(t *testing.T) {
	root := t.TempDir()
	path := writeFixture(t, root, "model.janet", "(def disk-value 1)\n")
	uri := PathToURI(path)
	index, err := BuildIndex(context.Background(), root, map[string]string{uri: "(def memory-value 2)\n"})
	if err != nil {
		t.Fatal(err)
	}
	definitions := index.TopLevelDefinitions(uri)
	if len(definitions) != 1 || definitions[0].Name != "memory-value" {
		t.Fatalf("definitions = %#v, want memory overlay", definitions)
	}
}

func writeFixture(t *testing.T, root, name, source string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func occurrenceNames(occurrences []Occurrence) []string {
	result := make([]string, 0, len(occurrences))
	for _, occurrence := range occurrences {
		result = append(result, occurrence.Name)
	}
	return result
}

func replacementTexts(changes map[string][]TextReplacement) []string {
	var result []string
	for _, replacements := range changes {
		for _, replacement := range replacements {
			result = append(result, replacement.NewText)
		}
	}
	sort.Strings(result)
	return result
}

func hasCompletion(items []Completion, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}
