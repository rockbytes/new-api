// schema-check is a conservative source guard for rolling releases, not a database diff.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var (
	formalRelease = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	devRelease    = regexp.MustCompile(`^dev-[0-9a-f]{7,40}$`)
	// Match DDL strings, not Go methods like db.Create (a data write).
	ddl = regexp.MustCompile(`(?i)\b(?:ALTER\s+(?:TABLE|INDEX|TYPE|SCHEMA|SEQUENCE|DATABASE|VIEW)\b|CREATE\s+(?:(?:OR REPLACE|UNIQUE|TEMPORARY|TEMP|MATERIALIZED)\s+)*(?:TABLE|INDEX|TYPE|SCHEMA|SEQUENCE|DATABASE|VIEW|TRIGGER|EXTENSION|FUNCTION|PROCEDURE|POLICY|DOMAIN)\b|DROP\s+(?:TABLE|INDEX|TYPE|SCHEMA|SEQUENCE|DATABASE|VIEW|TRIGGER|EXTENSION|FUNCTION|PROCEDURE|POLICY|DOMAIN)\b|TRUNCATE\b|RENAME\s+TABLE\b|COMMENT\s+ON\b)`)
)

func git(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		if failure, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(failure.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(out), nil
}

func releaseRef(version string) (string, error) {
	switch {
	case formalRelease.MatchString(version):
		return "refs/tags/" + version, nil
	case devRelease.MatchString(version):
		return strings.TrimPrefix(version, "dev-"), nil
	default:
		return "", fmt.Errorf("unrecognized running release %q", version)
	}
}

func migrationName(name string) bool {
	name = strings.ToLower(name)
	return name == "tablename" || strings.Contains(name, "migrat")
}

func schemaDecls(file, source string) ([]string, error) {
	fs := token.NewFileSet()
	syntax, err := parser.ParseFile(fs, file, source, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var sensitive []string
	for _, decl := range syntax.Decls {
		found := false
		if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.TYPE && strings.HasPrefix(file, "model/") {
			for _, spec := range gen.Specs {
				if _, ok := spec.(*ast.TypeSpec).Type.(*ast.StructType); ok {
					found = true
					break
				}
			}
		}
		ast.Inspect(decl, func(node ast.Node) bool {
			if found {
				return false
			}
			switch n := node.(type) {
			case *ast.Field:
				found = n.Tag != nil && strings.Contains(n.Tag.Value, `gorm:"`)
			case *ast.Ident:
				found = migrationName(n.Name)
			case *ast.BasicLit:
				if n.Kind == token.STRING {
					text, err := strconv.Unquote(n.Value)
					found = err != nil || ddl.MatchString(text)
				}
			}
			return !found
		})
		if found {
			var formatted bytes.Buffer
			if err := format.Node(&formatted, fs, decl); err != nil {
				return nil, err
			}
			sensitive = append(sensitive, formatted.String())
		}
	}
	return sensitive, nil
}

func checkChanges(base, target string) error {
	changes, err := git("diff", "--name-status", "-z", "--no-renames", base, target, "--")
	if err != nil || changes == "" {
		return err
	}
	entries := strings.Split(strings.TrimSuffix(changes, "\x00"), "\x00")
	if len(entries)%2 != 0 {
		return fmt.Errorf("unexpected Git diff output")
	}
	var blocked []string
	for i := 0; i < len(entries); i += 2 {
		status, file := entries[i], entries[i+1]
		name := path.Base(file)
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		if migrationName(file) || strings.EqualFold(path.Ext(file), ".sql") || slices.Contains([]string{"go.mod", "go.sum", "go.work", "go.work.sum"}, name) {
			blocked = append(blocked, file+" (migration, SQL, or Go dependency change)")
			continue
		}
		if !strings.HasSuffix(file, ".go") {
			continue
		}
		if status != "A" && status != "D" && status != "M" {
			return fmt.Errorf("cannot inspect %s change to %s", status, file)
		}
		var before, after []string
		if status != "A" {
			source, err := git("show", base+":"+file)
			if err != nil {
				return err
			}
			before, err = schemaDecls(file, source)
			if err != nil {
				return err
			}
		}
		if status != "D" {
			source, err := git("show", target+":"+file)
			if err != nil {
				return err
			}
			after, err = schemaDecls(file, source)
			if err != nil {
				return err
			}
		}
		if !slices.Equal(before, after) {
			blocked = append(blocked, file+" (schema-related declarations changed)")
		}
	}
	if len(blocked) > 0 {
		return fmt.Errorf("schema review required:\n  %s", strings.Join(blocked, "\n  "))
	}
	return nil
}

func run(current, targetRef string) error {
	currentRef, err := releaseRef(current)
	if err != nil {
		return err
	}
	base, err := git("rev-parse", "--verify", "--end-of-options", currentRef+"^{commit}")
	if err != nil {
		return err
	}
	target, err := git("rev-parse", "--verify", "--end-of-options", targetRef+"^{commit}")
	if err != nil {
		return err
	}
	base, target = strings.TrimSpace(base), strings.TrimSpace(target)
	if base == target {
		return fmt.Errorf("running release and target resolve to the same commit %s", target)
	}
	if _, err := git("merge-base", "--is-ancestor", base, target); err != nil {
		return fmt.Errorf("running release is not a verified ancestor of target: %w", err)
	}
	if err := checkChanges(base, target); err != nil {
		return err
	}
	fmt.Printf("Schema guard passed: %s (%s) -> %s\n", current, base, target)
	return nil
}

func main() {
	current := flag.String("current", "", "release directory name of the running binary")
	target := flag.String("target", "HEAD", "target Git revision")
	flag.Parse()
	if err := run(*current, *target); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
