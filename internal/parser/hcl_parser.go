// Package parser reads HCL files and produces model.Project values.
//
// We deliberately avoid gohcl: it requires an *hcl.EvalContext even though our
// HCL contains only literal values (no interpolation, no variables). Manual
// decoding via Body.Blocks/JustAttributes is simpler and has no EvalContext
// requirement.
package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"gitlab.com/lunaris-bot/metaci/internal/model"
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

	// Iterate ALL top-level items of the body so we get both attributes and
	// blocks, delegating each to the proper extractor.
	for _, item := range blk.Body.Attributes {
		val, err := literal(item.Expr)
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
		case "ci_repo":
			r, err := decodeRepoBlock(child)
			if err != nil {
				return model.Project{}, fmt.Errorf("ci_repo: %w", err)
			}
			proj.Repos = append(proj.Repos, r)
		case "ci_task":
			t, err := decodeTaskBlock(child)
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

func decodeRepoBlock(blk *hclsyntax.Block) (model.Repo, error) {
	if len(blk.Labels) != 1 {
		return model.Repo{}, fmt.Errorf("ci_repo requires 1 label")
	}
	r := model.Repo{Name: blk.Labels[0]}
	for name, attr := range attrsMap(blk.Body) {
		val, err := literal(attr.Expr)
		if err != nil {
			return model.Repo{}, fmt.Errorf("attribute %q: %w", name, err)
		}
		switch name {
		case "path":
			r.Path = val
		case "kind":
			r.Kind = val
		}
	}
	return r, nil
}

func decodeTaskBlock(blk *hclsyntax.Block) (model.Task, error) {
	if len(blk.Labels) != 1 {
		return model.Task{}, fmt.Errorf("ci_task requires 1 label")
	}
	t := model.Task{Name: blk.Labels[0]}

	// Top-level attributes of ci_task: trigger, manual, depends_on
	for name, attr := range attrsMap(blk.Body) {
		val, err := literal(attr.Expr)
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
				val, err := literal(attr.Expr)
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

// literal extracts the string value of a literal expression. We support:
//   - string literals (e.g.  "foo")
//   - tuple expressions (lists of literals), comma-separated
//   - bool literals
//
// We deliberately do NOT support variable interpolation (functions, refs),
// because pipeline configuration should be declarative and reproducible.
func literal(expr hcl.Expression) (string, error) {
	switch e := expr.(type) {
	case *hclsyntax.TemplateExpr:
		if len(e.Parts) == 1 {
			if ts, ok := e.Parts[0].(*hclsyntax.LiteralValueExpr); ok {
				return toString(ts.Val)
			}
		}
		return "", fmt.Errorf("interpolated strings are not supported in pipeline config")
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

// executeZero is a placeholder used for the optional check above.
type executeZero = model.ExecuteSpec

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
		if proj.Name == "" && p.Name != "" {
			proj.Name = p.Name
		}
		if p.DefaultImage != "" {
			proj.DefaultImage = p.DefaultImage
		}
	}
	return proj, nil
}

func formatDiags(diags hcl.Diagnostics, filename string) error {
	var sb strings.Builder
	for _, d := range diags {
		sb.WriteString(fmt.Sprintf("[%v] %s:%d:%d: %s\n",
			d.Severity, filename, d.Subject.Start.Line, d.Subject.Start.Column, d.Summary))
	}
	return fmt.Errorf("%s", sb.String())
}
