package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaGuardReleaseRange(t *testing.T) {
	t.Chdir(t.TempDir())
	gitCommand := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
		return string(out)
	}
	write := func(path, text string) {
		t.Helper()
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
		require.NoError(t, os.WriteFile(path, []byte(text), 0644))
	}
	commit := func(message string) {
		t.Helper()
		gitCommand("add", ".")
		gitCommand("-c", "user.name=Schema Guard Test", "-c", "user.email=schema-test@example.invalid", "-c", "commit.gpgSign=false", "commit", "-m", message)
	}
	gitCommand("init", "-b", "base")
	write("model/option.go", `package model
type Option struct { Key string; Value string }
func initStorage() { db.AutoMigrate(&Option{}) }
func writeOption(o Option) { db.Create(&o) }
`)
	commit("release baseline")
	gitCommand("-c", "tag.gpgSign=false", "tag", "-a", "v1.0.29", "-m", "release")
	baseHash, err := git("rev-parse", "--short", "HEAD")
	require.NoError(t, err)
	write("model/group_notes.go", `package model
func save() { db.Create(&Option{}); _ = "Could not create notes" }
`)
	write("model/group_notes_test.go", `package model
func fixture() { db.AutoMigrate(&Option{}); db.Exec("DROP TABLE fixture") }
`)
	commit("ordinary data writes and test fixtures")
	require.NoError(t, run("v1.0.29", "HEAD"))
	require.NoError(t, run("dev-"+strings.TrimSpace(baseHash), "HEAD"))
	assert.ErrorContains(t, run("v1.0.28", "HEAD"), "rev-parse")
	assert.ErrorContains(t, run("v1.0.29", "base~25"), "rev-parse")
	assert.ErrorContains(t, run("v1.0.29", "v1.0.29"), "same commit")

	write("service/db.go", "package service\nfunc setup() { db.Exec(`create unique\nindex group_note_idx on options (key)`) }\n")
	commit("production DDL")
	assert.ErrorContains(t, run("v1.0.29", "HEAD"), "service/db.go")
	gitCommand("rm", "service/db.go")
	write("model/option.go", `package model
type Option struct { Key string; Value string; Extra string }
func initStorage() { db.AutoMigrate(&Option{}) }
func writeOption(o Option) { db.Create(&o) }
`)
	commit("implicit column change")
	assert.ErrorContains(t, run("v1.0.29", "HEAD"), "model/option.go")
	write("model/option.go", `package model
type Option struct { Key string; Value string }
func writeOption(o Option) { db.Create(&o) }
`)
	commit("remove existing migration")
	assert.ErrorContains(t, run("v1.0.29", "HEAD"), "model/option.go")

	gitCommand("branch", "-f", "v1.0.29", "HEAD")
	assert.ErrorContains(t, run("v1.0.29", "HEAD"), "model/option.go", "a same-named branch must not shadow the release tag")
}
