package janet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type SymbolID struct {
	URI    string
	Offset int
}

type Definition struct {
	ID            SymbolID
	Name          string
	Kind          string
	URI           string
	Start, End    int
	Signature     string
	Documentation string
	Exported      bool
}

type Occurrence struct {
	ID          SymbolID
	URI         string
	Start, End  int
	Name        string
	Declaration bool
}

type Completion struct {
	Name string
	Kind string
}

type Index struct {
	Root        string
	Documents   map[string]*Document
	Definitions map[SymbolID]Definition
	Occurrences map[SymbolID][]Occurrence
	byPosition  map[string]map[int]Occurrence
	globals     map[string]map[string]SymbolID
	models      map[string]*documentModel
}

type documentModel struct {
	document     *Document
	definitions  map[string]SymbolID
	imports      []moduleImport
	bindings     []binding
	declarations map[int]SymbolID
	skip         map[int]bool
	atoms        []*Node
}

type moduleImport struct {
	Prefix string
	URI    string
}

type binding struct {
	Name       string
	ID         SymbolID
	Start, End int
}

func BuildIndex(ctx context.Context, root string, overlays map[string]string) (*Index, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	paths, err := workspaceFiles(ctx, absoluteRoot)
	if err != nil {
		return nil, err
	}

	documents := make(map[string]*Document, len(paths)+len(overlays))
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("read %s: %w", path, readErr)
		}
		uri := PathToURI(path)
		documents[uri] = Parse(uri, path, string(source))
	}
	for uri, source := range overlays {
		path, pathErr := URIToPath(uri)
		if pathErr != nil {
			return nil, fmt.Errorf("decode document URI %q: %w", uri, pathErr)
		}
		canonicalURI := PathToURI(path)
		documents[canonicalURI] = Parse(canonicalURI, path, source)
	}

	index := &Index{
		Root:        absoluteRoot,
		Documents:   documents,
		Definitions: make(map[SymbolID]Definition),
		Occurrences: make(map[SymbolID][]Occurrence),
		byPosition:  make(map[string]map[int]Occurrence),
		globals:     make(map[string]map[string]SymbolID),
		models:      make(map[string]*documentModel),
	}
	for uri, document := range documents {
		model := analyzeDocument(document)
		index.models[uri] = model
		index.globals[uri] = model.definitions
		for _, id := range model.definitions {
			definition := definitionAt(model, id.Offset)
			index.Definitions[id] = definition
		}
		for _, local := range model.bindings {
			token, ok := tokenStartingAt(document.Tokens, local.ID.Offset)
			if !ok {
				continue
			}
			index.Definitions[local.ID] = Definition{
				ID:        local.ID,
				Name:      local.Name,
				Kind:      "variable",
				URI:       document.URI,
				Start:     token.Start,
				End:       token.End,
				Signature: local.Name,
			}
		}
	}
	for _, model := range index.models {
		model.imports = analyzeImports(index.Root, model.document)
		index.resolveDocument(model)
	}
	for id := range index.Occurrences {
		sort.Slice(index.Occurrences[id], func(left, right int) bool {
			a := index.Occurrences[id][left]
			b := index.Occurrences[id][right]
			if a.URI != b.URI {
				return a.URI < b.URI
			}
			return a.Start < b.Start
		})
	}
	return index, nil
}

func (index *Index) Document(uri string) *Document {
	path, err := URIToPath(uri)
	if err != nil {
		return nil
	}
	return index.Documents[PathToURI(path)]
}

func (index *Index) OccurrenceAt(uri string, offset int) (Occurrence, bool) {
	document := index.Document(uri)
	if document == nil {
		return Occurrence{}, false
	}
	positions := index.byPosition[document.URI]
	if occurrence, ok := positions[offset]; ok {
		return occurrence, true
	}
	for _, occurrence := range positions {
		if occurrence.Start <= offset && offset <= occurrence.End {
			return occurrence, true
		}
	}
	return Occurrence{}, false
}

func (index *Index) DefinitionFor(occurrence Occurrence) (Definition, bool) {
	definition, ok := index.Definitions[occurrence.ID]
	return definition, ok
}

func (index *Index) References(id SymbolID, includeDeclaration bool) []Occurrence {
	all := index.Occurrences[id]
	if includeDeclaration {
		return append([]Occurrence(nil), all...)
	}
	result := make([]Occurrence, 0, len(all))
	for _, occurrence := range all {
		if !occurrence.Declaration {
			result = append(result, occurrence)
		}
	}
	return result
}

func (index *Index) Rename(id SymbolID, newName string) (map[string][]TextReplacement, error) {
	if !validRename(newName) {
		return nil, fmt.Errorf("%q is not a valid unqualified Janet symbol", newName)
	}
	definition, ok := index.Definitions[id]
	if ok {
		if existing, exists := index.globals[definition.URI][newName]; exists && existing != id {
			return nil, fmt.Errorf("%s already defines %s", definition.URI, newName)
		}
	}

	changes := make(map[string][]TextReplacement)
	for _, occurrence := range index.Occurrences[id] {
		replacement := newName
		if slash := strings.LastIndexByte(occurrence.Name, '/'); slash >= 0 {
			replacement = occurrence.Name[:slash+1] + newName
		}
		changes[occurrence.URI] = append(changes[occurrence.URI], TextReplacement{
			Start:   occurrence.Start,
			End:     occurrence.End,
			NewText: replacement,
		})
	}
	return changes, nil
}

type TextReplacement struct {
	Start, End int
	NewText    string
}

func (index *Index) Completions(uri string, offset int) []Completion {
	document := index.Document(uri)
	if document == nil {
		return nil
	}
	model := index.models[document.URI]
	items := make(map[string]string)
	for name, id := range model.definitions {
		items[name] = index.Definitions[id].Kind
	}
	for _, candidate := range model.bindings {
		if candidate.Start <= offset && offset < candidate.End {
			items[candidate.Name] = "variable"
		}
	}
	for _, imported := range model.imports {
		for name, id := range index.globals[imported.URI] {
			definition := index.Definitions[id]
			if definition.Exported {
				items[imported.Prefix+name] = definition.Kind
			}
		}
	}
	result := make([]Completion, 0, len(items))
	for name, kind := range items {
		result = append(result, Completion{Name: name, Kind: kind})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result
}

func (index *Index) TopLevelDefinitions(uri string) []Definition {
	document := index.Document(uri)
	if document == nil {
		return nil
	}
	result := make([]Definition, 0, len(index.globals[document.URI]))
	for _, id := range index.globals[document.URI] {
		result = append(result, index.Definitions[id])
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Start < result[right].Start })
	return result
}

func (index *Index) SignatureAt(uri string, offset int) (Definition, bool) {
	document := index.Document(uri)
	if document == nil {
		return Definition{}, false
	}
	var best *Node
	var visit func(*Node)
	visit = func(node *Node) {
		if node.Kind == NodeList && node.Start <= offset && offset <= node.End && len(node.Children) > 0 {
			if best == nil || node.End-node.Start < best.End-best.Start {
				best = node
			}
		}
		for _, child := range node.Children {
			visit(child)
		}
	}
	visit(document.Root)
	if best == nil {
		return Definition{}, false
	}
	head, ok := best.Children[0].Atom(document.Tokens)
	if !ok {
		return Definition{}, false
	}
	occurrence, ok := index.OccurrenceAt(document.URI, head.Start)
	if !ok {
		return Definition{}, false
	}
	return index.DefinitionFor(occurrence)
}

func analyzeDocument(document *Document) *documentModel {
	model := &documentModel{
		document:     document,
		definitions:  make(map[string]SymbolID),
		declarations: make(map[int]SymbolID),
		skip:         make(map[int]bool),
	}
	for _, form := range document.Root.Children {
		collectTopLevelDefinition(model, form)
		markImportSyntax(model, form)
	}
	for _, form := range document.Root.Children {
		collectBindings(model, form, true, document.Root.End)
	}
	walkAtoms(document.Root, false, func(node *Node, quoted bool) {
		if quoted {
			model.skip[node.Start] = true
		}
		model.atoms = append(model.atoms, node)
	})
	return model
}

func collectTopLevelDefinition(model *documentModel, form *Node) {
	head, ok := listHead(model.document, form)
	if !ok || !isDefinitionForm(head.Text) || len(form.Children) < 2 {
		return
	}
	name, ok := form.Children[1].Atom(model.document.Tokens)
	if !ok || name.Kind != TokenSymbol || !resolvableSymbol(name.Text) {
		return
	}
	id := SymbolID{URI: model.document.URI, Offset: name.Start}
	model.definitions[name.Text] = id
	model.declarations[name.Start] = id
}

func definitionAt(model *documentModel, offset int) Definition {
	for _, form := range model.document.Root.Children {
		head, ok := listHead(model.document, form)
		if !ok || len(form.Children) < 2 {
			continue
		}
		name, nameOK := form.Children[1].Atom(model.document.Tokens)
		if !nameOK || name.Start != offset {
			continue
		}
		documentation := ""
		for _, child := range form.Children[2:] {
			token, atom := child.Atom(model.document.Tokens)
			if atom && token.Kind == TokenString {
				documentation = token.Value()
				break
			}
			if child.Kind == NodeVector {
				break
			}
		}
		signature := strings.TrimSpace(model.document.Source[form.Start:min(form.End, len(model.document.Source))])
		if newline := strings.IndexByte(signature, '\n'); newline >= 0 {
			signature = signature[:newline]
		}
		return Definition{
			ID:            SymbolID{URI: model.document.URI, Offset: offset},
			Name:          name.Text,
			Kind:          definitionKind(head.Text),
			URI:           model.document.URI,
			Start:         name.Start,
			End:           name.End,
			Signature:     signature,
			Documentation: documentation,
			Exported:      !strings.HasSuffix(head.Text, "-"),
		}
	}
	return Definition{}
}

func collectBindings(model *documentModel, form *Node, topLevel bool, parentEnd int) {
	if form.Kind != NodeList || len(form.Children) == 0 {
		for _, child := range form.Children {
			collectBindings(model, child, false, form.End)
		}
		return
	}
	head, ok := listHead(model.document, form)
	if !ok {
		return
	}
	if isBindingForm(head.Text) {
		model.skip[head.Start] = true
	}

	switch head.Text {
	case "defn", "defn-", "defmacro", "defmacro-":
		if !topLevel && len(form.Children) > 1 {
			addLocalBinding(model, form.Children[1], form.Children[1].End, parentEnd)
		}
		if parameters := firstVector(form.Children[2:]); parameters != nil {
			addPatternBindings(model, parameters, parameters.End, form.End)
		}
	case "fn":
		if parameters := firstVector(form.Children[1:]); parameters != nil {
			addPatternBindings(model, parameters, parameters.End, form.End)
		}
	case "let", "loop", "with", "with-dyns", "with-syms":
		if len(form.Children) > 1 && form.Children[1].Kind == NodeVector {
			bindings := form.Children[1].Children
			for pair := 0; pair < len(bindings); pair += 2 {
				start := bindings[pair].End
				if pair+1 < len(bindings) {
					start = bindings[pair+1].End
				}
				addPatternBindings(model, bindings[pair], start, form.End)
			}
		}
	case "if-let", "when-let":
		if len(form.Children) > 1 && form.Children[1].Kind == NodeVector && len(form.Children[1].Children) > 0 {
			bindings := form.Children[1].Children
			start := form.Children[1].End
			if len(bindings) > 1 {
				start = bindings[1].End
			}
			addPatternBindings(model, bindings[0], start, form.End)
		}
	case "each", "for":
		if len(form.Children) > 2 {
			startNode := form.Children[2]
			if head.Text == "for" && len(form.Children) > 3 {
				startNode = form.Children[3]
			}
			addPatternBindings(model, form.Children[1], startNode.End, form.End)
		}
	case "eachk":
		if len(form.Children) > 3 {
			addPatternBindings(model, form.Children[1], form.Children[3].End, form.End)
			addPatternBindings(model, form.Children[2], form.Children[3].End, form.End)
		}
	case "def", "def-", "var", "defdyn":
		if !topLevel && len(form.Children) > 1 {
			addLocalBinding(model, form.Children[1], form.End, parentEnd)
		}
	}

	for _, child := range form.Children {
		collectBindings(model, child, false, form.End)
	}
}

func addPatternBindings(model *documentModel, pattern *Node, start, end int) {
	if token, ok := pattern.Atom(model.document.Tokens); ok {
		if token.Kind == TokenSymbol && resolvableSymbol(token.Text) && token.Text != "_" && token.Text != "&" {
			addBinding(model, token, start, end)
		}
		return
	}
	for _, child := range pattern.Children {
		addPatternBindings(model, child, start, end)
	}
}

func addLocalBinding(model *documentModel, node *Node, start, end int) {
	token, ok := node.Atom(model.document.Tokens)
	if !ok || token.Kind != TokenSymbol || !resolvableSymbol(token.Text) {
		return
	}
	addBinding(model, token, start, end)
}

func addBinding(model *documentModel, token Token, start, end int) {
	id := SymbolID{URI: model.document.URI, Offset: token.Start}
	model.bindings = append(model.bindings, binding{Name: token.Text, ID: id, Start: start, End: end})
	model.declarations[token.Start] = id
}

func markImportSyntax(model *documentModel, form *Node) {
	head, ok := listHead(model.document, form)
	if !ok || (head.Text != "import" && head.Text != "use") {
		return
	}
	for _, child := range form.Children {
		if token, atom := child.Atom(model.document.Tokens); atom {
			model.skip[token.Start] = true
		}
	}
}

func analyzeImports(root string, document *Document) []moduleImport {
	var imports []moduleImport
	for _, form := range document.Root.Children {
		head, ok := listHead(document, form)
		if !ok || (head.Text != "import" && head.Text != "use") || len(form.Children) < 2 {
			continue
		}
		moduleToken, moduleOK := form.Children[1].Atom(document.Tokens)
		if !moduleOK {
			continue
		}
		moduleName := moduleToken.Value()
		targetPath := resolveModule(root, document.Path, moduleName)
		if targetPath == "" {
			continue
		}
		prefix := strings.TrimSuffix(filepath.Base(moduleName), filepath.Ext(moduleName)) + "/"
		if head.Text == "use" {
			prefix = ""
		}
		for option := 2; option+1 < len(form.Children); option++ {
			key, keyOK := form.Children[option].Symbol(document.Tokens)
			if !keyOK {
				continue
			}
			value, valueOK := form.Children[option+1].Atom(document.Tokens)
			if !valueOK {
				continue
			}
			switch key {
			case ":as":
				prefix = value.Value() + "/"
			case ":prefix":
				prefix = value.Value()
			}
		}
		imports = append(imports, moduleImport{Prefix: prefix, URI: PathToURI(targetPath)})
	}
	sort.Slice(imports, func(left, right int) bool {
		return len(imports[left].Prefix) > len(imports[right].Prefix)
	})
	return imports
}

func (index *Index) resolveDocument(model *documentModel) {
	index.byPosition[model.document.URI] = make(map[int]Occurrence)
	walkAtoms(model.document.Root, false, func(node *Node, quoted bool) {
		token, ok := node.Atom(model.document.Tokens)
		if !ok || token.Kind != TokenSymbol || quoted || model.skip[token.Start] || !resolvableSymbol(token.Text) {
			return
		}
		id, declaration, resolved := index.resolveToken(model, token)
		if !resolved {
			return
		}
		occurrence := Occurrence{
			ID:          id,
			URI:         model.document.URI,
			Start:       token.Start,
			End:         token.End,
			Name:        token.Text,
			Declaration: declaration,
		}
		index.Occurrences[id] = append(index.Occurrences[id], occurrence)
		index.byPosition[model.document.URI][token.Start] = occurrence
	})
}

func (index *Index) resolveToken(model *documentModel, token Token) (SymbolID, bool, bool) {
	if id, ok := model.declarations[token.Start]; ok {
		return id, true, true
	}
	var selected *binding
	for candidateIndex := range model.bindings {
		candidate := &model.bindings[candidateIndex]
		if candidate.Name != token.Text || token.Start < candidate.Start || token.Start >= candidate.End {
			continue
		}
		if selected == nil || candidate.Start > selected.Start ||
			(candidate.Start == selected.Start && candidate.ID.Offset > selected.ID.Offset) {
			selected = candidate
		}
	}
	if selected != nil {
		return selected.ID, false, true
	}
	if id, ok := model.definitions[token.Text]; ok {
		return id, false, true
	}
	for _, imported := range model.imports {
		if !strings.HasPrefix(token.Text, imported.Prefix) {
			continue
		}
		name := strings.TrimPrefix(token.Text, imported.Prefix)
		if name == "" {
			continue
		}
		if id, ok := index.globals[imported.URI][name]; ok && index.Definitions[id].Exported {
			return id, false, true
		}
	}
	return SymbolID{}, false, false
}

func tokenStartingAt(tokens []Token, offset int) (Token, bool) {
	for _, token := range tokens {
		if token.Start == offset {
			return token, true
		}
	}
	return Token{}, false
}

func walkAtoms(node *Node, quoted bool, visit func(*Node, bool)) {
	quoted = quoted || node.Quoted
	if node.Kind == NodeAtom {
		visit(node, quoted)
		return
	}
	for _, child := range node.Children {
		walkAtoms(child, quoted, visit)
	}
}

func listHead(document *Document, node *Node) (Token, bool) {
	if node == nil || node.Kind != NodeList || len(node.Children) == 0 {
		return Token{}, false
	}
	token, ok := node.Children[0].Atom(document.Tokens)
	return token, ok && token.Kind == TokenSymbol
}

func firstVector(nodes []*Node) *Node {
	for _, node := range nodes {
		if node.Kind == NodeVector {
			return node
		}
	}
	return nil
}

func isDefinitionForm(name string) bool {
	switch name {
	case "def", "def-", "defn", "defn-", "defmacro", "defmacro-", "defdyn", "var":
		return true
	default:
		return false
	}
}
func isBindingForm(name string) bool {
	switch name {
	case "def", "def-", "defn", "defn-", "defmacro", "defmacro-", "defdyn", "var",
		"fn", "let", "loop", "with", "with-dyns", "with-syms", "if-let", "when-let",
		"each", "for", "eachk":
		return true
	default:
		return false
	}
}

func definitionKind(form string) string {
	if strings.Contains(form, "fn") || strings.Contains(form, "macro") {
		return "function"
	}
	return "variable"
}

func resolvableSymbol(name string) bool {
	if name == "" || strings.HasPrefix(name, ":") {
		return false
	}
	switch name {
	case "nil", "true", "false", "&", "_":
		return false
	}
	number := strings.ReplaceAll(name, "_", "")
	if _, err := strconv.ParseFloat(number, 64); err == nil {
		return false
	}
	return true
}

func validRename(name string) bool {
	if !resolvableSymbol(name) || strings.Contains(name, "/") {
		return false
	}
	tokens, diagnostics := lex(name)
	return len(diagnostics) == 0 && len(tokens) == 1 && tokens[0].Kind == TokenSymbol && tokens[0].Text == name
}

func resolveModule(root, sourcePath, moduleName string) string {
	var base string
	if strings.HasPrefix(moduleName, ".") {
		base = filepath.Join(filepath.Dir(sourcePath), filepath.FromSlash(moduleName))
	} else {
		base = filepath.Join(root, filepath.FromSlash(moduleName))
	}
	candidates := []string{base, base + ".janet", filepath.Join(base, "init.janet")}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			absolute, absErr := filepath.Abs(candidate)
			if absErr == nil {
				return absolute
			}
		}
	}
	return ""
}

func workspaceFiles(ctx context.Context, root string) ([]string, error) {
	gitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	command := exec.CommandContext(gitCtx, "git", "-C", root, "ls-files", "--cached", "--others", "--exclude-standard", "--", "*.janet")
	output, gitErr := command.Output()
	if gitErr == nil {
		lines := bytes.Split(output, []byte{'\n'})
		paths := make([]string, 0, len(lines))
		for _, line := range lines {
			if len(line) == 0 {
				continue
			}
			paths = append(paths, filepath.Join(root, filepath.FromSlash(string(line))))
		}
		sort.Strings(paths)
		return paths, nil
	}
	if errors.Is(gitCtx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("enumerate Janet workspace: %w", gitCtx.Err())
	}

	ignored := map[string]bool{
		".git": true, ".hg": true, ".svn": true,
		"bin": true, "build": true, "jpm_tree": true,
		"node_modules": true, "reference": true, "vendor": true,
	}
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() && path != root && ignored[entry.Name()] {
			return filepath.SkipDir
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".janet") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}
