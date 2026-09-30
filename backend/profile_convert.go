package main

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// legacyEngineProfilesDir is where profiles used to be dropped as JSON files.
// Only the convert-profile-rules command and the startup guard read it.
func legacyEngineProfilesDir() string {
	return cacheFilePath("ENGINE_PROFILES_DIR", "plugins/engine-profiles")
}

// legacyProfileFiles lists the *.json files in dir, sorted. A missing
// directory is the normal state of a fresh install and yields none.
func legacyProfileFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

// checkForLegacyProfileFiles is the startup guard: an empty profile table
// beside a directory that still holds profile files means the operator has
// not converted yet, and starting would quietly show them no profiles.
func checkForLegacyProfileFiles(store *profileStore, dir string) error {
	n, err := store.count()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	names, err := legacyProfileFiles(dir)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return nil
	}
	return fmt.Errorf(
		"found %d equipment profile file(s) in %s and no profiles in the database - profiles are no longer read from files. "+
			"Import them first with `helmcentral convert-profile-rules --apply` (Docker Compose: "+
			"`docker compose run --rm helmcentral /app/helmcentral convert-profile-rules --apply`), then restart. "+
			"Run it without --apply to preview what it will do",
		len(names), dir)
}

// conversionStep is one part of the convert-profile-rules run. Every step
// runs in the one transaction, so a failure anywhere leaves nothing changed.
type conversionStep struct {
	name string
	run  func(tx *sql.Tx, out io.Writer, apply bool) error
}

// conversionOptions are the command's flags.
type conversionOptions struct {
	// DetachUnresolved turns a copied maintenance rule whose item has no
	// usable profile into a hand rule, instead of aborting the run.
	DetachUnresolved bool
}

// convertProfileRulesSteps is the list later phases add to.
func convertProfileRulesSteps(dir string, opts conversionOptions) []conversionStep {
	return []conversionStep{
		{name: "import profiles", run: func(tx *sql.Tx, out io.Writer, apply bool) error {
			return importLegacyProfilesTx(tx, dir, out, apply)
		}},
		{name: "convert maintenance rules", run: func(tx *sql.Tx, out io.Writer, apply bool) error {
			return convertMaintenanceRulesTx(tx, opts, out, apply)
		}},
	}
}

// runConversion runs every step in one transaction. Without apply the
// transaction is rolled back at the end, so a dry run exercises exactly the
// checks a real run does.
func runConversion(store *profileStore, steps []conversionStep, out io.Writer, apply bool) error {
	tx, err := store.db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	for _, step := range steps {
		if err := step.run(tx, out, apply); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}
	if !apply {
		fmt.Fprintln(out, "convert-profile-rules: dry run, nothing written. Run again with --apply to write these changes")
		return nil
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func importLegacyProfilesTx(tx *sql.Tx, dir string, out io.Writer, apply bool) error {
	names, err := legacyProfileFiles(dir)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Fprintf(out, "convert-profile-rules: no profile files in %s - nothing to import\n", dir)
		return nil
	}
	verb := "would import"
	if apply {
		verb = "imported"
	}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		profile, err := parseProfileDocument(data)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := insertProfileTx(tx, profile, profileBasedOn{}); err != nil {
			if errors.Is(err, errProfileExists) {
				return fmt.Errorf("%s: a profile with id %q already exists", name, profile.ID)
			}
			return fmt.Errorf("%s: %w", name, err)
		}
		fmt.Fprintf(out, "convert-profile-rules: %s %s (%s, %q) from %s\n", verb, profile.ID, profile.Kind, profile.Name, name)
	}
	return nil
}

// runConvertProfileRulesCommand is the `convert-profile-rules` subcommand
// (same one-off, operator-run shape as migrate-db).
func runConvertProfileRulesCommand(args []string) int {
	apply := false
	var opts conversionOptions
	for _, arg := range args {
		switch arg {
		case "--apply":
			apply = true
		case "--detach-unresolved":
			opts.DetachUnresolved = true
		default:
			fmt.Fprintf(os.Stderr, "convert-profile-rules: unknown argument %q (usage: convert-profile-rules [--apply] [--detach-unresolved])\n", arg)
			return 2
		}
	}
	// openDocumentStore, not newDocumentStore: this command is what repairs a
	// database newDocumentStore refuses to start on.
	ds, err := openDocumentStore(helmcentralDBPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "convert-profile-rules: %v\n", err)
		return 1
	}
	defer ds.Close()
	store, err := newProfileStore(ds)
	if err != nil {
		fmt.Fprintf(os.Stderr, "convert-profile-rules: %v\n", err)
		return 1
	}
	if err := runConversion(store, convertProfileRulesSteps(legacyEngineProfilesDir(), opts), os.Stdout, apply); err != nil {
		fmt.Fprintf(os.Stderr, "convert-profile-rules: %v\n", err)
		return 1
	}
	return 0
}
