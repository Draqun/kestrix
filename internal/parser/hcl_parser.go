// Package parser reads HCL files and produces model.Project values.
//
// We deliberately avoid gohcl: it requires an *hcl.EvalContext even though our
// HCL contains only literal values (no interpolation, no variables). Manual
// decoding via Body.Blocks/JustAttributes is simpler and has no EvalContext
// requirement.
package parser

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"kestrix/internal/domain"
	"kestrix/internal/model"
)

// ParseFile reads a single HCL file and returns its Project definition.
func ParseFile(path string) (model.Project, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return model.Project{}, fmt.Errorf("read %s: %w", path, err)
	}
	return ParseSource(src, path)
}

// ParseSource parses HCL source bytes into a Project.
func ParseSource(src []byte, filename string) (model.Project, error) {
	f, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return model.Project{}, formatDiags(diags, filename)
	}
	body := f.Body.(*hclsyntax.Body)

	var proj model.Project
	for _, blk := range body.Blocks {
		switch blk.Type {
		case "project":
			p, err := decodeProjectBlock(blk)
			if err != nil {
				return model.Project{}, fmt.Errorf("project block: %w", err)
			}
			proj = p
		default:
			// Top-level blocks must be `project`.
			return model.Project{}, fmt.Errorf("unsupported top-level block %q", blk.Type)
		}
	}
	return proj, nil
}

// decodeProjectBlock reads the inner attributes (name, default_image) and
// child blocks (ci_repo, ci_task) of a project block.
func decodeProjectBlock(blk *hclsyntax.Block) (model.Project, error) {
	if len(blk.Labels) != 1 {
		return model.Project{}, fmt.Errorf("project block requires exactly 1 label, got %d", len(blk.Labels))
	}
	proj := model.Project{Name: blk.Labels[0]}

	// Decode variable blocks first so we can build the interpolation evaluation context
	// before decoding project attributes, repos and tasks.
	for _, child := range blk.Body.Blocks {
		if child.Type == "variable" {
			v, err := decodeVariableBlock(child)
			if err != nil {
				return model.Project{}, fmt.Errorf("variable: %w", err)
			}
			proj.Variables = append(proj.Variables, v)
		}
	}

	vc := buildVarEvalContext(proj.Variables)

	// Iterate ALL top-level items of the body so we get both attributes and
	// blocks, delegating each to the proper extractor.
	for _, item := range blk.Body.Attributes {
		var val string
		var err error
		switch item.Name {
		case "default_image":
			val, err = evalInterpolatedString(item.Expr, vc, "default_image")
		default:
			val, err = literal(item.Expr, item.Name)
		}
		if err != nil {
			return model.Project{}, fmt.Errorf("attribute %q: %w", item.Name, err)
		}
		switch item.Name {
		case "default_image":
			proj.DefaultImage = val
		}
	}

	for _, child := range blk.Body.Blocks {
		switch child.Type {
		case "variable":
			// Already decoded in first pass.
			continue
		case "ci_repo":
			r, err := decodeRepoBlock(child, vc)
			if err != nil {
				return model.Project{}, fmt.Errorf("ci_repo: %w", err)
			}
			proj.Repos = append(proj.Repos, r)
		case "ci_task":
			t, err := decodeTaskBlock(child, vc)
			if err != nil {
				return model.Project{}, fmt.Errorf("ci_task: %w", err)
			}
			proj.Tasks = append(proj.Tasks, t)
		default:
			return model.Project{}, fmt.Errorf("unsupported child block %q in project", child.Type)
		}
	}
	return proj, nil
}

func decodeRepoBlock(blk *hclsyntax.Block, vctx ...*varEvalContext) (model.Repo, error) {
	// len(blk.Labels) != 1 is unreachable in practice: HCL raises its
	// own syntax error for `ci_repo {}` (no labels) and `ci_repo "a" "b"`
	// before this block-decoder runs. The guard stays as a defensive
	// check; the branch is documented as unreachable in parser tests.
	if len(blk.Labels) != 1 {
		return model.Repo{}, fmt.Errorf("ci_repo requires 1 label")
	}
	var vc *varEvalContext
	if len(vctx) > 0 && vctx[0] != nil {
		vc = vctx[0]
	} else {
		vc = buildVarEvalContext(nil)
	}

	r := model.Repo{Name: blk.Labels[0]}
	for name, attr := range attrsMap(blk.Body) {
		var val string
		var err error
		switch name {
		case "url":
			val, err = evalInterpolatedString(attr.Expr, vc, "url")
		case "ref":
			val, err = evalInterpolatedString(attr.Expr, vc, "ref")
		default:
			val, err = literal(attr.Expr, name)
		}
		if err != nil {
			return model.Repo{}, fmt.Errorf("attribute %q: %w", name, err)
		}
		switch name {
		case "path":
			r.Path = val
		case "kind":
			r.Kind = val
		case "url":
			r.URL = val
		case "ref":
			r.Ref = val
		case "credential_ref":
			r.CredentialRef = val
		}
	}
	if strings.TrimSpace(r.Path) == "" && strings.TrimSpace(r.URL) == "" {
		return model.Repo{}, fmt.Errorf("repository must define exactly one of path or url")
	}
	if strings.TrimSpace(r.Path) != "" && strings.TrimSpace(r.URL) != "" {
		return model.Repo{}, fmt.Errorf("repository must define exactly one of path or url")
	}
	if strings.TrimSpace(r.Path) != "" {
		clean := filepath.Clean(r.Path)
		if filepath.IsAbs(r.Path) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return model.Repo{}, fmt.Errorf("local repository %q path must be relative and stay within the primary checkout", r.Name)
		}
	}
	if strings.TrimSpace(r.URL) != "" && hasEmbeddedCredentials(r.URL) {
		return model.Repo{}, fmt.Errorf("repository url must not contain embedded credentials")
	}
	if strings.TrimSpace(r.URL) != "" && strings.TrimSpace(r.Ref) == "" {
		return model.Repo{}, fmt.Errorf("remote repository %q must define a non-empty ref", r.Name)
	}
	if strings.TrimSpace(r.Path) != "" && strings.TrimSpace(r.Ref) != "" {
		return model.Repo{}, fmt.Errorf("local repository %q must not define ref", r.Name)
	}
	return r, nil
}

func hasEmbeddedCredentials(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return true
	}
	if parsed.User == nil {
		return false
	}
	// `git@host:path` and `ssh://git@host/path` are normal SSH transport
	// forms. Passwords and userinfo on HTTPS URLs are credentials and are
	// rejected so they cannot leak through definitions or diagnostics.
	if parsed.Scheme == "ssh" && parsed.User.Username() != "" {
		_, hasPassword := parsed.User.Password()
		return hasPassword
	}
	return true
}

func decodeTaskBlock(blk *hclsyntax.Block, vctx ...*varEvalContext) (model.Task, error) {
	// len(blk.Labels) != 1 is unreachable in practice: HCL raises its
	// own syntax error for `ci_task {}` (no labels) and `ci_task "a" "b"`
	// before this block-decoder runs. Defensive guard only.
	if len(blk.Labels) != 1 {
		return model.Task{}, fmt.Errorf("ci_task requires 1 label")
	}
	var vc *varEvalContext
	if len(vctx) > 0 && vctx[0] != nil {
		vc = vctx[0]
	} else {
		vc = buildVarEvalContext(nil)
	}

	t := model.Task{Name: blk.Labels[0]}

	// Top-level attributes of ci_task: trigger, manual, depends_on, runner_tags, artifacts
	for name, attr := range attrsMap(blk.Body) {
		switch name {
		case "runner_tags":
			var items []string
			if tc, ok := attr.Expr.(*hclsyntax.TupleConsExpr); ok {
				for _, ex := range tc.Exprs {
					itemVal, err := evalInterpolatedString(ex, vc, "runner_tags")
					if err != nil {
						return model.Task{}, fmt.Errorf("task %q: runner_tags: %w", t.Name, err)
					}
					items = append(items, itemVal)
				}
			} else {
				itemVal, err := evalInterpolatedString(attr.Expr, vc, "runner_tags")
				if err != nil {
					return model.Task{}, fmt.Errorf("task %q: runner_tags: %w", t.Name, err)
				}
				items = splitList(itemVal)
			}
			tags, err := domain.NormalizeRunnerTags(items)
			if err != nil {
				return model.Task{}, fmt.Errorf("task %q: runner_tags: %w", t.Name, err)
			}
			t.RunnerTags = tags
			continue
		case "artifacts":
			var items []string
			if tc, ok := attr.Expr.(*hclsyntax.TupleConsExpr); ok {
				var err error
				items, err = tupleStrings(tc)
				if err != nil {
					return model.Task{}, fmt.Errorf("attribute %q: %w", name, err)
				}
			} else {
				val, err := literal(attr.Expr, name)
				if err != nil {
					return model.Task{}, fmt.Errorf("attribute %q: %w", name, err)
				}
				items = splitList(val)
			}
			if err := domain.ValidateArtifacts(t.Name, items); err != nil {
				return model.Task{}, err
			}
			t.Artifacts = items
			continue
		}

		val, err := literal(attr.Expr, name)
		if err != nil {
			return model.Task{}, fmt.Errorf("attribute %q: %w", name, err)
		}
		switch name {
		case "trigger":
			t.Trigger = val
		case "manual":
			t.Manual = val == "true"
		case "depends_on":
			t.DependsOn = splitList(val)
		}
	}

	// execute block (optional, at most one)
	for _, child := range blk.Body.Blocks {
		switch child.Type {
		case "execute":
			t.Execute = model.ExecuteSpec{}
			for name, attr := range attrsMap(child.Body) {
				var val string
				var err error
				switch name {
				case "image":
					val, err = evalInterpolatedString(attr.Expr, vc, "image")
				default:
					val, err = literal(attr.Expr, name)
				}
				if err != nil {
					return model.Task{}, fmt.Errorf("execute.%s: %w", name, err)
				}
				switch name {
				case "repo":
					t.Execute.Repo = val
				case "script":
					t.Execute.Script = val
				case "image":
					t.Execute.Image = val
				}
			}
		default:
			return model.Task{}, fmt.Errorf("unsupported child block %q in ci_task", child.Type)
		}
	}
	return t, nil
}

// attrsMap returns the block body's attributes as a name -> *Attribute map by
// reading the hclsyntax.Body.Attributes slice directly. We don't use
// JustAttributes because it errors out if the body contains any sub-blocks
// (which our bodies do).
func attrsMap(body *hclsyntax.Body) map[string]*hclsyntax.Attribute {
	out := map[string]*hclsyntax.Attribute{}
	for _, a := range body.Attributes {
		out[a.Name] = a
	}
	return out
}

// varEvalContext holds an evaluation context and variable index for resolving
// variable references in allowlisted fields.
type varEvalContext struct {
	evalCtx  *hcl.EvalContext
	varIndex map[string]model.Variable
}

// checkVariableEligibility verifies that a variable meets the eligibility rules
// defined in design D2 for interpolation into allowlisted fields.
func checkVariableEligibility(v model.Variable, fieldName string) error {
	prefix := ""
	if fieldName != "" {
		prefix = fmt.Sprintf("variable %q cannot be interpolated into %s: ", v.Key, fieldName)
	} else {
		prefix = fmt.Sprintf("variable %q: ", v.Key)
	}
	if v.SecretRef != "" {
		return fmt.Errorf("%ssecret variables cannot be interpolated", prefix)
	}
	if v.Masked {
		return fmt.Errorf("%smasked variables cannot be interpolated", prefix)
	}
	if v.Protected {
		return fmt.Errorf("%sprotected variables cannot be interpolated", prefix)
	}
	if v.Scope != "" && v.Scope != "project" {
		if v.Scope == "job" {
			return fmt.Errorf("%sjob-scoped variables cannot be interpolated", prefix)
		}
		if v.Scope == "repo" {
			return fmt.Errorf("%srepo-scoped variables cannot be interpolated", prefix)
		}
		return fmt.Errorf("%s%s-scoped variables cannot be interpolated", prefix, v.Scope)
	}
	if v.Value == "" && v.Default == "" {
		return fmt.Errorf("%svariable has no value or default", prefix)
	}
	return nil
}

func buildVarEvalContext(vars []model.Variable) *varEvalContext {
	varIndex := make(map[string]model.Variable, len(vars))
	eligibleMap := make(map[string]cty.Value)

	for _, v := range vars {
		varIndex[v.Key] = v
		if err := checkVariableEligibility(v, ""); err == nil {
			val := v.Value
			if val == "" {
				val = v.Default
			}
			eligibleMap[v.Key] = cty.StringVal(val)
		}
	}

	varObj := cty.EmptyObjectVal
	if len(eligibleMap) > 0 {
		varObj = cty.ObjectVal(eligibleMap)
	}

	evalCtx := &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"var": varObj,
		},
	}
	return &varEvalContext{
		evalCtx:  evalCtx,
		varIndex: varIndex,
	}
}

func checkVariableRef(varName string, varIndex map[string]model.Variable, fieldName string) (model.Variable, error) {
	v, ok := varIndex[varName]
	if !ok {
		return model.Variable{}, fmt.Errorf("variable %q cannot be interpolated into %s: unknown variable; variable/scoped references are not supported", varName, fieldName)
	}
	if err := checkVariableEligibility(v, fieldName); err != nil {
		return model.Variable{}, err
	}
	return v, nil
}

func extractVarTraversal(traversal hcl.Traversal) (string, error) {
	if len(traversal) != 2 {
		return "", fmt.Errorf("only variable references are supported")
	}
	root, ok := traversal[0].(hcl.TraverseRoot)
	if !ok || root.Name != "var" {
		return "", fmt.Errorf("only variable references are supported")
	}
	attr, ok := traversal[1].(hcl.TraverseAttr)
	if !ok {
		return "", fmt.Errorf("only variable references are supported")
	}
	return attr.Name, nil
}

func evalInterpolatedString(expr hcl.Expression, vc *varEvalContext, fieldName string) (string, error) {
	if expr == nil {
		return "", nil
	}
	if wrap, ok := expr.(*hclsyntax.TemplateWrapExpr); ok {
		expr = wrap.Wrapped
	}

	switch e := expr.(type) {
	case *hclsyntax.LiteralValueExpr:
		return toString(e.Val)
	case *hclsyntax.ScopeTraversalExpr:
		varName, err := extractVarTraversal(e.Traversal)
		if err != nil {
			return "", err
		}
		if _, err := checkVariableRef(varName, vc.varIndex, fieldName); err != nil {
			return "", err
		}
		val, _ := e.Value(vc.evalCtx)
		return toString(val)
	case *hclsyntax.TemplateExpr:
		for _, part := range e.Parts {
			switch p := part.(type) {
			case *hclsyntax.LiteralValueExpr:
				// static literal part in template
			case *hclsyntax.ScopeTraversalExpr:
				varName, err := extractVarTraversal(p.Traversal)
				if err != nil {
					return "", err
				}
				if _, err := checkVariableRef(varName, vc.varIndex, fieldName); err != nil {
					return "", err
				}
			default:
				return "", fmt.Errorf("only variable references and string templates are supported")
			}
		}
		val, _ := e.Value(vc.evalCtx)
		return toString(val)
	case *hclsyntax.FunctionCallExpr:
		return "", fmt.Errorf("only variable references and string templates are supported")
	case *hclsyntax.ObjectConsExpr:
		return "", fmt.Errorf("object literals are not supported here")
	default:
		return "", fmt.Errorf("only variable references and string templates are supported")
	}
}

// literal extracts the string value of a literal expression. We support:
//   - string literals (e.g.  "foo")
//   - tuple expressions (lists of literals), comma-separated
//   - bool literals
//
// We deliberately do NOT support variable interpolation (functions, refs),
// because pipeline configuration should be declarative and reproducible.
func literal(expr hcl.Expression, fieldName ...string) (string, error) {
	switch e := expr.(type) {
	case *hclsyntax.TemplateWrapExpr:
		var field string
		if len(fieldName) > 0 {
			field = fieldName[0]
		}
		switch field {
		case "script":
			return "", fmt.Errorf("interpolated strings are not supported in script; use $VAR for environment variables, or $${VAR} for a literal ${")
		case "trigger":
			return "", fmt.Errorf("interpolated strings are not supported in trigger; triggers use their own expression syntax")
		default:
			return "", fmt.Errorf("interpolated strings are not supported in pipeline config")
		}
	case *hclsyntax.TemplateExpr:
		if len(e.Parts) == 1 {
			if ts, ok := e.Parts[0].(*hclsyntax.LiteralValueExpr); ok {
				return toString(ts.Val)
			}
		}
		var field string
		if len(fieldName) > 0 {
			field = fieldName[0]
		}
		switch field {
		case "script":
			return "", fmt.Errorf("interpolated strings are not supported in script; use $VAR for environment variables, or $${VAR} for a literal ${")
		case "trigger":
			return "", fmt.Errorf("interpolated strings are not supported in trigger; triggers use their own expression syntax")
		default:
			return "", fmt.Errorf("interpolated strings are not supported in pipeline config")
		}
	case *hclsyntax.LiteralValueExpr:
		return toString(e.Val)
	case *hclsyntax.TupleConsExpr:
		parts, err := tupleStrings(e)
		if err != nil {
			return "", err
		}
		return strings.Join(parts, ","), nil
	case *hclsyntax.ObjectConsExpr:
		return "", fmt.Errorf("object literals are not supported here")
	case *hclsyntax.SplatExpr, *hclsyntax.RelativeTraversalExpr, *hclsyntax.ScopeTraversalExpr:
		var field string
		if len(fieldName) > 0 {
			field = fieldName[0]
		}
		if field == "script" {
			return "", fmt.Errorf("variable/scoped references are not supported in script; use $VAR for environment variables")
		}
		if field == "trigger" {
			return "", fmt.Errorf("variable/scoped references are not supported in trigger; triggers use their own expression syntax")
		}
		return "", fmt.Errorf("variable/scoped references are not supported; use literal values only")
	default:
		return "", fmt.Errorf("unsupported expression type %T", expr)
	}
}

func tupleStrings(e *hclsyntax.TupleConsExpr) ([]string, error) {
	out := make([]string, 0, len(e.Exprs))
	for _, ex := range e.Exprs {
		s, err := literal(ex)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func toString(v interface{}) (string, error) {
	// hclsyntax.LiteralValueExpr.Val is a cty.Value.
	switch x := v.(type) {
	case cty.Value:
		if x.Type() == cty.String {
			return x.AsString(), nil
		}
		if x.Type() == cty.Bool {
			if x.True() {
				return "true", nil
			}
			return "false", nil
		}
		return "", fmt.Errorf("unsupported cty type %s", x.Type().FriendlyName())
	case string:
		return x, nil
	case bool:
		if x {
			return "true", nil
		}
		return "false", nil
	default:
		return "", fmt.Errorf("unsupported literal type %T", v)
	}
}

func splitList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ParseDir reads all .tf files in a directory and merges them into one Project.
func ParseDir(dir string) (model.Project, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return model.Project{}, fmt.Errorf("read dir %s: %w", dir, err)
	}
	var proj model.Project
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tf") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		p, err := ParseFile(path)
		if err != nil {
			return model.Project{}, fmt.Errorf("parse %s: %w", path, err)
		}
		proj.Repos = append(proj.Repos, p.Repos...)
		proj.Tasks = append(proj.Tasks, p.Tasks...)
		proj.Variables = append(proj.Variables, p.Variables...)
		if p.Name != "" {
			if proj.Name == "" {
				proj.Name = p.Name
			} else if proj.Name != p.Name {
				return model.Project{}, fmt.Errorf("parse %s: project name %q does not match %q", path, p.Name, proj.Name)
			}
		}
		if p.DefaultImage != "" {
			proj.DefaultImage = p.DefaultImage
		}
	}
	return proj, nil
}

func decodeVariableBlock(blk *hclsyntax.Block) (model.Variable, error) {
	// len(blk.Labels) != 1 is unreachable in practice: HCL raises its
	// own syntax error for `variable {}` (no labels) and `variable "k" "v"`
	// before this block-decoder runs. Defensive guard only.
	if len(blk.Labels) != 1 {
		return model.Variable{}, fmt.Errorf("variable requires 1 label (key name)")
	}
	v := model.Variable{Key: blk.Labels[0]}
	for name, attr := range attrsMap(blk.Body) {
		val, err := literal(attr.Expr)
		if err != nil {
			return model.Variable{}, fmt.Errorf("attribute %q: %w", name, err)
		}
		switch name {
		case "value":
			v.Value = val
		case "default":
			v.Default = val
		case "secret_ref":
			v.SecretRef = val
		case "scope":
			v.Scope = val
		case "repo":
			v.Repo = val
		case "job":
			v.Job = val
		case "env":
			v.Env = val
		case "protected":
			v.Protected = val == "true"
		case "masked":
			v.Masked = val == "true"
		}
	}
	if v.SecretRef != "" {
		var conflicts []string
		if v.Value != "" {
			conflicts = append(conflicts, "value")
		}
		if v.Default != "" {
			conflicts = append(conflicts, "default")
		}
		if len(conflicts) > 0 {
			return model.Variable{}, fmt.Errorf("variable %q: secret_ref cannot be combined with %s", v.Key, strings.Join(conflicts, ", "))
		}
		v.Masked = true
	}
	switch v.Scope {
	case "repo":
		if strings.TrimSpace(v.Repo) == "" {
			return model.Variable{}, fmt.Errorf("variable %q: scope \"repo\" requires repo", v.Key)
		}
	case "job":
		if strings.TrimSpace(v.Repo) == "" || strings.TrimSpace(v.Job) == "" {
			return model.Variable{}, fmt.Errorf("variable %q: scope \"job\" requires repo and job", v.Key)
		}
	}
	return v, nil
}

func formatDiags(diags hcl.Diagnostics, filename string) error {
	var sb strings.Builder
	for _, d := range diags {
		sb.WriteString(fmt.Sprintf("[%v] %s:%d:%d: %s\n",
			d.Severity, filename, d.Subject.Start.Line, d.Subject.Start.Column, d.Summary))
	}
	return fmt.Errorf("%s", sb.String())
}
