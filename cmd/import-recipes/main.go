package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/importer"
)

func main() {
	if err := run(); err != nil {
		GoLog.Errorf("fatal: %v", err)
		os.Exit(1)
	}
}

func run() error {
	dir := flag.String("dir", "", "directory to scan for *.json recipe files (recursive)")
	file := flag.String("file", "", "single JSON file to import")
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "PostgreSQL connection URL (default: $DATABASE_URL)")
	dryRun := flag.Bool("dry-run", false, "parse and validate only; do not write to DB")
	flag.Parse()

	if *dir == "" && *file == "" {
		return errors.New("provide -dir <path> or -file <path>")
	}
	if *dbURL == "" && !*dryRun {
		return errors.New("DATABASE_URL not set and -db not provided")
	}

	// Parse recipes.
	var recipes []*importer.MIRecipe
	var parseErrs []importer.ParseError

	if *dir != "" {
		recipes, parseErrs = importer.ParseDir(*dir)
	} else {
		rec, err := importer.ParseFile(*file)
		if err != nil {
			parseErrs = []importer.ParseError{{File: *file, Err: err}}
		} else {
			recipes = []*importer.MIRecipe{rec}
		}
	}

	for _, pe := range parseErrs {
		GoLog.Warnf("parse error %s: %v", pe.File, pe.Err)
	}
	GoLog.Infof("parsed %d recipes (%d parse errors)", len(recipes), len(parseErrs))

	if *dryRun {
		// Validate and print result without hitting DB.
		valid, invalid := 0, 0
		for _, rec := range recipes {
			if problems := importer.ValidateFormat(rec); len(problems) != 0 {
				GoLog.Warnf("invalid %s: %v", rec.SourceFile, problems)
				invalid++
			} else {
				valid++
			}
		}
		GoLog.Infof("dry-run: %d valid, %d invalid", valid, invalid)
		return nil
	}

	// Connect to DB.
	ctx := context.Background()
	database, err := db.New(ctx, *dbURL)
	if err != nil {
		return fmt.Errorf("connect db: %w", err)
	}
	defer database.Close()

	// Run import.
	imp := importer.New(database)
	result, err := imp.Run(ctx, recipes)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}

	// Print result.
	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(out))

	GoLog.Infof("done: imported=%d skipped=%d errors=%d",
		result.Imported, result.Skipped, len(result.Errors))
	return nil
}
