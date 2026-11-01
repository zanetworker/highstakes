package analyzer

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars/c"
	"github.com/odvcencio/gotreesitter/grammars/c_sharp"
	"github.com/odvcencio/gotreesitter/grammars/cpp"
	"github.com/odvcencio/gotreesitter/grammars/java"
	"github.com/odvcencio/gotreesitter/grammars/javascript"
	"github.com/odvcencio/gotreesitter/grammars/kotlin"
	"github.com/odvcencio/gotreesitter/grammars/ruby"
	"github.com/odvcencio/gotreesitter/grammars/rust"
	"github.com/odvcencio/gotreesitter/grammars/tsx"
	"github.com/odvcencio/gotreesitter/grammars/typescript"
)

// maxParseBytes caps the file size handed to tree-sitter. Larger files are
// usually generated or minified and fall back to the line heuristics.
const maxParseBytes = 1 << 20

// parseTimeoutMicros bounds a single parse so one pathological file cannot stall analysis.
const parseTimeoutMicros = 5_000_000

// parseResult holds what one tree-sitter pass extracts from a file.
type parseResult struct {
	Cyclomatic int
	Cognitive  int
	Functions  int
	Exported   int
	// Imports are raw, language-specific import specs; resolveImports maps them to repo files.
	Imports []string
	// Packages are the Java/Kotlin package or C# namespaces the file declares.
	Packages []string
	// Symbols are the qualified names of top-level Kotlin declarations and C#
	// namespace-level types, which need not match the file name.
	Symbols []string
	// fileNamespace is the C# file-scoped namespace, whose types are its siblings.
	fileNamespace string
}

type set map[string]bool

func newSet(items ...string) set {
	s := make(set, len(items))
	for _, i := range items {
		s[i] = true
	}
	return s
}

// langSpec maps a grammar's node types onto the decision points the Go path counts.
type langSpec struct {
	grammar   func() *ts.Language
	functions set // named functions and methods: +1 cyclomatic each, reset nesting
	closures  set // lambdas and blocks: raise nesting, no increment
	branches  set // if-like nodes
	loops     set
	switches  set // switch/match nodes: cognitive only
	arms      set // case/match arms: cyclomatic only
	catches   set
	ternaries set
	boolNodes set // nodes whose &&/||/and/or operator tokens each add a branch
	elseIfs   set // node types that are always an else-if (Ruby elsif)
	exported  func(n *ts.Node, src []byte, l *ts.Language) int
	imports   func(n *ts.Node, src []byte, l *ts.Language, r *parseResult)
}

var boolOperators = newSet("&&", "||", "and", "or")

var jsSpec = langSpec{
	functions: newSet("function_declaration", "generator_function_declaration", "method_definition"),
	closures:  newSet("arrow_function", "function_expression", "function", "generator_function"),
	branches:  newSet("if_statement"),
	loops:     newSet("for_statement", "for_in_statement", "while_statement", "do_statement"),
	switches:  newSet("switch_statement"),
	arms:      newSet("switch_case", "switch_default"),
	catches:   newSet("catch_clause"),
	ternaries: newSet("ternary_expression"),
	boolNodes: newSet("binary_expression"),
	exported:  jsExported,
	imports:   jsImports,
}

var specs = map[string]*langSpec{
	"rust": {
		grammar:   rust.Language,
		functions: newSet("function_item"),
		closures:  newSet("closure_expression"),
		branches:  newSet("if_expression"),
		loops:     newSet("for_expression", "while_expression", "loop_expression"),
		switches:  newSet("match_expression"),
		arms:      newSet("match_arm"),
		boolNodes: newSet("binary_expression"),
		exported:  rustExported,
		imports:   rustImports,
	},
	"typescript": withGrammar(jsSpec, typescript.Language),
	"tsx":        withGrammar(jsSpec, tsx.Language),
	"javascript": withGrammar(jsSpec, javascript.Language),
	"java": {
		grammar:   java.Language,
		functions: newSet("method_declaration", "constructor_declaration", "compact_constructor_declaration"),
		closures:  newSet("lambda_expression"),
		branches:  newSet("if_statement"),
		loops:     newSet("for_statement", "enhanced_for_statement", "while_statement", "do_statement"),
		switches:  newSet("switch_expression", "switch_statement"),
		arms:      newSet("switch_block_statement_group", "switch_rule"),
		catches:   newSet("catch_clause"),
		ternaries: newSet("ternary_expression"),
		boolNodes: newSet("binary_expression"),
		exported:  modifierExported(newSet("class_declaration", "interface_declaration", "enum_declaration", "record_declaration", "annotation_type_declaration", "method_declaration", "constructor_declaration", "field_declaration"), "modifiers"),
		imports:   javaImports,
	},
	"c_sharp": {
		grammar:   c_sharp.Language,
		functions: newSet("method_declaration", "constructor_declaration", "local_function_statement"),
		closures:  newSet("lambda_expression", "anonymous_method_expression"),
		branches:  newSet("if_statement"),
		loops:     newSet("for_statement", "foreach_statement", "while_statement", "do_statement"),
		switches:  newSet("switch_statement", "switch_expression"),
		arms:      newSet("switch_section", "switch_expression_arm"),
		catches:   newSet("catch_clause"),
		ternaries: newSet("conditional_expression"),
		boolNodes: newSet("binary_expression"),
		exported:  modifierExported(newSet("class_declaration", "struct_declaration", "interface_declaration", "enum_declaration", "record_declaration", "method_declaration", "constructor_declaration", "property_declaration", "field_declaration", "delegate_declaration", "event_declaration"), "modifier"),
		imports:   csharpImports,
	},
	"c":   cFamily(c.Language),
	"cpp": cFamily(cpp.Language),
	"ruby": {
		grammar:   ruby.Language,
		functions: newSet("method", "singleton_method"),
		closures:  newSet("block", "do_block", "lambda"),
		branches:  newSet("if", "unless", "if_modifier", "unless_modifier", "elsif"),
		loops:     newSet("while", "until", "for", "while_modifier", "until_modifier"),
		switches:  newSet("case", "case_match"),
		arms:      newSet("when", "in_clause"),
		catches:   newSet("rescue", "rescue_modifier"),
		ternaries: newSet("conditional"),
		boolNodes: newSet("binary"),
		elseIfs:   newSet("elsif"),
		exported:  rubyExported,
		imports:   rubyImports,
	},
	"kotlin": {
		grammar:   kotlin.Language,
		functions: newSet("function_declaration", "secondary_constructor"),
		closures:  newSet("lambda_literal", "anonymous_function"),
		branches:  newSet("if_expression"),
		loops:     newSet("for_statement", "while_statement", "do_while_statement"),
		switches:  newSet("when_expression"),
		arms:      newSet("when_entry"),
		catches:   newSet("catch_block"),
		boolNodes: newSet("conjunction_expression", "disjunction_expression"),
		exported:  kotlinExported,
		imports:   kotlinImports,
	},
}

func withGrammar(s langSpec, g func() *ts.Language) *langSpec {
	s.grammar = g
	return &s
}

func cFamily(g func() *ts.Language) *langSpec {
	return &langSpec{
		grammar:   g,
		functions: newSet("function_definition"),
		closures:  newSet("lambda_expression"),
		branches:  newSet("if_statement"),
		loops:     newSet("for_statement", "for_range_loop", "while_statement", "do_statement"),
		switches:  newSet("switch_statement"),
		arms:      newSet("case_statement"),
		catches:   newSet("catch_clause"),
		ternaries: newSet("conditional_expression"),
		boolNodes: newSet("binary_expression"),
		exported:  cExported,
		imports:   cImports,
	}
}

// grammarKey picks the grammar for a detected language; "" means tree-sitter does not handle it.
func grammarKey(lang, path string) string {
	switch lang {
	case "Rust":
		return "rust"
	case "TypeScript":
		if strings.EqualFold(filepath.Ext(path), ".tsx") {
			return "tsx"
		}
		return "typescript"
	case "JavaScript":
		return "javascript"
	case "Java":
		return "java"
	case "C#":
		return "c_sharp"
	case "C":
		return "c"
	case "C++":
		return "cpp"
	case "Ruby":
		return "ruby"
	case "Kotlin":
		return "kotlin"
	}
	return ""
}

var (
	poolsMu sync.Mutex
	pools   = map[string]*ts.ParserPool{}
)

// parserPool returns a concurrency-safe parser pool, loading the grammar on first use.
func parserPool(key string) *ts.ParserPool {
	poolsMu.Lock()
	defer poolsMu.Unlock()
	if p, ok := pools[key]; ok {
		return p
	}
	p := ts.NewParserPool(specs[key].grammar(), ts.WithParserPoolTimeoutMicros(parseTimeoutMicros))
	pools[key] = p
	return p
}

// parseSource parses src once with tree-sitter and returns complexity, symbol and import facts.
// It returns an error for unsupported languages, oversized files, and parser failures so the
// caller can fall back to the line heuristics.
func parseSource(lang, path string, src []byte) (res *parseResult, err error) {
	key := grammarKey(lang, path)
	if key == "" {
		return nil, fmt.Errorf("no tree-sitter grammar for language %q", lang)
	}
	if len(src) > maxParseBytes {
		return nil, fmt.Errorf("%s is %d bytes, over the %d byte parse limit", path, len(src), maxParseBytes)
	}
	defer func() {
		// The parser is young; never let a grammar bug take down the whole analysis.
		if r := recover(); r != nil {
			res, err = nil, fmt.Errorf("tree-sitter panic parsing %s: %v", path, r)
		}
	}()

	pool := parserPool(key)
	tree, err := pool.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	defer tree.Release()

	w := &walker{spec: specs[key], lang: pool.Language(), src: src, res: &parseResult{}}
	w.walk(tree.RootNode(), nil, 0)
	return w.res, nil
}

type walker struct {
	spec *langSpec
	lang *ts.Language
	src  []byte
	res  *parseResult
}

// walk tracks the parent itself: Node.Parent() can return a hidden
// supertype node (JS "statement") instead of the visible parent.
func (w *walker) walk(n *ts.Node, parent *ts.Node, nesting int) {
	if n == nil {
		return
	}
	if !n.IsNamed() {
		// Keyword tokens can share a node type name ("if", "case", "class" in Ruby).
		return
	}
	s := w.spec
	t := n.Type(w.lang)
	childNesting := nesting

	switch {
	case s.functions[t] || (s.closures[t] && isJSFunctionValue(n, w.lang)):
		w.res.Functions++
		w.res.Cyclomatic++
		childNesting = 0
	case s.closures[t]:
		childNesting = nesting + 1
	case s.branches[t]:
		w.res.Cyclomatic++
		if s.elseIfs[t] || isElseIf(n, parent, w.lang) {
			w.res.Cognitive++
		} else {
			w.res.Cognitive += 1 + nesting
			childNesting = nesting + 1
		}
	case s.loops[t], s.catches[t], s.ternaries[t]:
		w.res.Cyclomatic++
		w.res.Cognitive += 1 + nesting
		childNesting = nesting + 1
	case s.switches[t]:
		w.res.Cognitive += 1 + nesting
		childNesting = nesting + 1
	case s.arms[t]:
		w.res.Cyclomatic++
	case s.boolNodes[t]:
		for i := 0; i < n.ChildCount(); i++ {
			if ch := n.Child(i); !ch.IsNamed() && boolOperators[ch.Type(w.lang)] {
				w.res.Cyclomatic++
			}
		}
	}

	if s.exported != nil {
		w.res.Exported += s.exported(n, w.src, w.lang)
	}
	if s.imports != nil {
		s.imports(n, w.src, w.lang, w.res)
	}

	for i := 0; i < n.ChildCount(); i++ {
		w.walk(n.Child(i), n, childNesting)
	}
}

// isJSFunctionValue reports whether an arrow/function expression is bound to a variable,
// which makes it a named function in practice (const f = () => {}).
func isJSFunctionValue(n *ts.Node, l *ts.Language) bool {
	p := n.Parent()
	return p != nil && p.Type(l) == "variable_declarator"
}

// isElseIf reports whether an if node continues an else branch, which costs no nesting.
func isElseIf(n, p *ts.Node, l *ts.Language) bool {
	if p == nil {
		return false
	}
	switch p.Type(l) {
	case "else_clause": // Rust, JS/TS, C, C++
		return true
	case "control_structure_body": // Kotlin wraps the else branch
		prev := p.PrevSibling()
		return prev != nil && prev.Type(l) == "else"
	}
	prev := n.PrevSibling() // Java, C#: the alternative follows an else token
	return prev != nil && prev.Type(l) == "else"
}

func childOfType(n *ts.Node, l *ts.Language, types ...string) *ts.Node {
	for i := 0; i < n.ChildCount(); i++ {
		ch := n.Child(i)
		t := ch.Type(l)
		for _, want := range types {
			if t == want {
				return ch
			}
		}
	}
	return nil
}

func compact(s string) string { return strings.Join(strings.Fields(s), "") }

// stringLiteral returns the contents of a quoted string node, or "" for templates/interpolation.
func stringLiteral(n *ts.Node, src []byte, l *ts.Language) string {
	if n == nil || n.Type(l) != "string" {
		return ""
	}
	text := n.Text(src)
	if len(text) < 2 || strings.ContainsAny(text, "`") || strings.Contains(text, "#{") {
		return ""
	}
	return text[1 : len(text)-1]
}

// Exported-symbol counters.

var rustItems = newSet("function_item", "struct_item", "enum_item", "trait_item", "type_item", "const_item", "static_item", "union_item", "mod_item")

func rustExported(n *ts.Node, src []byte, l *ts.Language) int {
	if !rustItems[n.Type(l)] {
		return 0
	}
	if v := childOfType(n, l, "visibility_modifier"); v != nil && v.Text(src) == "pub" {
		return 1
	}
	return 0
}

func jsExported(n *ts.Node, src []byte, l *ts.Language) int {
	if n.Type(l) != "export_statement" {
		return 0
	}
	if d := n.ChildByFieldName("declaration", l); d != nil {
		if dt := d.Type(l); dt == "lexical_declaration" || dt == "variable_declaration" {
			count := 0
			for i := 0; i < d.NamedChildCount(); i++ {
				if d.NamedChild(i).Type(l) == "variable_declarator" {
					count++
				}
			}
			return count
		}
		return 1
	}
	if clause := childOfType(n, l, "export_clause"); clause != nil {
		count := 0
		for i := 0; i < clause.NamedChildCount(); i++ {
			if clause.NamedChild(i).Type(l) == "export_specifier" {
				count++
			}
		}
		return count
	}
	if childOfType(n, l, "default") != nil {
		return 1
	}
	return 0 // export * from "..."
}

// modifierExported counts declarations whose modifier child (Java "modifiers", C# "modifier") says public.
func modifierExported(decls set, modifierType string) func(*ts.Node, []byte, *ts.Language) int {
	return func(n *ts.Node, src []byte, l *ts.Language) int {
		if !decls[n.Type(l)] {
			return 0
		}
		for i := 0; i < n.ChildCount(); i++ {
			ch := n.Child(i)
			if ch.Type(l) != modifierType {
				continue
			}
			for _, word := range strings.Fields(ch.Text(src)) {
				if word == "public" {
					return 1
				}
			}
		}
		return 0
	}
}

func cExported(n *ts.Node, src []byte, l *ts.Language) int {
	if n.Type(l) != "function_definition" {
		return 0
	}
	if p := n.Parent(); p == nil || (p.Type(l) != "translation_unit" && p.Type(l) != "declaration_list") {
		return 0
	}
	if sc := childOfType(n, l, "storage_class_specifier"); sc != nil && sc.Text(src) == "static" {
		return 0
	}
	return 1
}

func rubyExported(n *ts.Node, src []byte, l *ts.Language) int {
	switch n.Type(l) {
	case "class", "module", "singleton_method":
		return 1
	case "method":
		for prev := n.PrevSibling(); prev != nil; prev = prev.PrevSibling() {
			if prev.Type(l) == "identifier" {
				if v := prev.Text(src); v == "private" || v == "protected" {
					return 0
				}
			}
		}
		return 1
	}
	return 0
}

var kotlinDecls = newSet("class_declaration", "object_declaration", "function_declaration", "property_declaration")

func kotlinExported(n *ts.Node, src []byte, l *ts.Language) int {
	if !kotlinDecls[n.Type(l)] {
		return 0
	}
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Type(l) == "function_body" {
			return 0 // local declaration
		}
	}
	if mods := childOfType(n, l, "modifiers"); mods != nil {
		if v := childOfType(mods, l, "visibility_modifier"); v != nil && v.Text(src) != "public" {
			return 0
		}
	}
	return 1
}

// Import extractors.

func rustImports(n *ts.Node, src []byte, l *ts.Language, r *parseResult) {
	switch n.Type(l) {
	case "mod_item":
		if childOfType(n, l, "declaration_list") == nil {
			if name := n.ChildByFieldName("name", l); name != nil {
				r.Imports = append(r.Imports, "mod:"+name.Text(src))
			}
		}
	case "use_declaration":
		if arg := n.ChildByFieldName("argument", l); arg != nil {
			r.Imports = append(r.Imports, expandRustUse(arg, "", src, l)...)
		}
	}
}

func joinRust(prefix, s string) string {
	if prefix == "" {
		return s
	}
	if s == "" || s == "self" {
		return prefix
	}
	return prefix + "::" + s
}

// expandRustUse flattens a use tree such as crate::a::{b, c::d as e} into full paths.
func expandRustUse(n *ts.Node, prefix string, src []byte, l *ts.Language) []string {
	switch n.Type(l) {
	case "use_as_clause":
		if p := n.ChildByFieldName("path", l); p != nil {
			return expandRustUse(p, prefix, src, l)
		}
	case "use_wildcard":
		if n.NamedChildCount() > 0 {
			return []string{joinRust(prefix, compact(n.NamedChild(0).Text(src)))}
		}
		return []string{prefix}
	case "scoped_use_list":
		p := prefix
		if path := n.ChildByFieldName("path", l); path != nil {
			p = joinRust(prefix, compact(path.Text(src)))
		}
		if list := n.ChildByFieldName("list", l); list != nil {
			return expandRustUse(list, p, src, l)
		}
	case "use_list":
		var out []string
		for i := 0; i < n.NamedChildCount(); i++ {
			out = append(out, expandRustUse(n.NamedChild(i), prefix, src, l)...)
		}
		return out
	default:
		return []string{joinRust(prefix, compact(n.Text(src)))}
	}
	return nil
}

func jsImports(n *ts.Node, src []byte, l *ts.Language, r *parseResult) {
	var spec string
	switch n.Type(l) {
	case "import_statement", "export_statement":
		spec = stringLiteral(n.ChildByFieldName("source", l), src, l)
	case "import_require_clause":
		spec = stringLiteral(childOfType(n, l, "string"), src, l)
	case "call_expression":
		fn := n.ChildByFieldName("function", l)
		if fn == nil || !(fn.Type(l) == "import" || (fn.Type(l) == "identifier" && fn.Text(src) == "require")) {
			return
		}
		if args := n.ChildByFieldName("arguments", l); args != nil && args.NamedChildCount() > 0 {
			spec = stringLiteral(args.NamedChild(0), src, l)
		}
	}
	if spec != "" {
		r.Imports = append(r.Imports, spec)
	}
}

func javaImports(n *ts.Node, src []byte, l *ts.Language, r *parseResult) {
	switch n.Type(l) {
	case "package_declaration":
		if id := childOfType(n, l, "scoped_identifier", "identifier"); id != nil {
			r.Packages = append(r.Packages, compact(id.Text(src)))
		}
	case "import_declaration":
		id := childOfType(n, l, "scoped_identifier", "identifier")
		if id == nil {
			return
		}
		spec := compact(id.Text(src))
		if childOfType(n, l, "asterisk") != nil {
			spec += ".*"
		}
		r.Imports = append(r.Imports, spec)
	}
}

func kotlinImports(n *ts.Node, src []byte, l *ts.Language, r *parseResult) {
	switch n.Type(l) {
	case "package_header":
		if id := childOfType(n, l, "identifier"); id != nil {
			r.Packages = append(r.Packages, compact(id.Text(src)))
		}
	case "class_declaration", "object_declaration", "function_declaration", "property_declaration":
		if p := n.Parent(); p == nil || p.Type(l) != "source_file" {
			return
		}
		name := childOfType(n, l, "type_identifier", "simple_identifier")
		if v := childOfType(n, l, "variable_declaration"); v != nil {
			name = childOfType(v, l, "simple_identifier")
		}
		if name == nil {
			return
		}
		sym := name.Text(src)
		if len(r.Packages) > 0 {
			sym = r.Packages[0] + "." + sym
		}
		r.Symbols = append(r.Symbols, sym)
	case "import_header":
		id := childOfType(n, l, "identifier")
		if id == nil {
			return
		}
		spec := compact(id.Text(src))
		if childOfType(n, l, "wildcard_import") != nil {
			spec += ".*"
		}
		r.Imports = append(r.Imports, spec)
	}
}

func csharpImports(n *ts.Node, src []byte, l *ts.Language, r *parseResult) {
	switch n.Type(l) {
	case "using_directive":
		// For "using X = A.B;" the target is the last name; for "using A.B;" it is the only one.
		var last *ts.Node
		for i := 0; i < n.NamedChildCount(); i++ {
			if ch := n.NamedChild(i); ch.Type(l) == "identifier" || ch.Type(l) == "qualified_name" {
				last = ch
			}
		}
		if last != nil {
			r.Imports = append(r.Imports, compact(last.Text(src)))
		}
	case "namespace_declaration", "file_scoped_namespace_declaration":
		name := n.ChildByFieldName("name", l)
		if name == nil {
			return
		}
		ns := compact(name.Text(src))
		for p := n.Parent(); p != nil; p = p.Parent() {
			if p.Type(l) == "namespace_declaration" {
				if outer := p.ChildByFieldName("name", l); outer != nil {
					ns = compact(outer.Text(src)) + "." + ns
				}
			}
		}
		r.Packages = append(r.Packages, ns)
		if n.Type(l) == "file_scoped_namespace_declaration" {
			r.fileNamespace = ns
		}
	default:
		if csharpTypes[n.Type(l)] {
			if sym := csharpTypeSymbol(n, src, l, r.fileNamespace); sym != "" {
				r.Symbols = append(r.Symbols, sym)
			}
		}
	}
}

var csharpTypes = newSet("class_declaration", "struct_declaration", "interface_declaration", "enum_declaration", "record_declaration", "delegate_declaration")

// csharpTypeSymbol qualifies a namespace-level type with its enclosing
// namespaces (or the file-scoped one); nested types return "".
func csharpTypeSymbol(n *ts.Node, src []byte, l *ts.Language, fileNamespace string) string {
	name := n.ChildByFieldName("name", l)
	if name == nil {
		return ""
	}
	var nss []string
	for p := n.Parent(); p != nil; p = p.Parent() {
		switch {
		case csharpTypes[p.Type(l)]:
			return ""
		case p.Type(l) == "namespace_declaration":
			if ns := p.ChildByFieldName("name", l); ns != nil {
				nss = append([]string{compact(ns.Text(src))}, nss...)
			}
		}
	}
	if len(nss) == 0 && fileNamespace != "" {
		nss = []string{fileNamespace}
	}
	return strings.Join(append(nss, name.Text(src)), ".")
}

func cImports(n *ts.Node, src []byte, l *ts.Language, r *parseResult) {
	if n.Type(l) != "preproc_include" {
		return
	}
	if p := n.ChildByFieldName("path", l); p != nil {
		r.Imports = append(r.Imports, strings.TrimSpace(p.Text(src)))
	}
}

func rubyImports(n *ts.Node, src []byte, l *ts.Language, r *parseResult) {
	if n.Type(l) != "call" || n.ChildByFieldName("receiver", l) != nil {
		return
	}
	method := n.ChildByFieldName("method", l)
	if method == nil {
		return
	}
	kind := method.Text(src)
	if kind != "require" && kind != "require_relative" {
		return
	}
	args := n.ChildByFieldName("arguments", l)
	if args == nil || args.NamedChildCount() == 0 {
		return
	}
	if spec := stringLiteral(args.NamedChild(0), src, l); spec != "" {
		r.Imports = append(r.Imports, kind+":"+spec)
	}
}
