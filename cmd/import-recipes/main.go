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
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "PostgreSQL connection URL (default: $DATABASE_URL)")
	assetsDir := flag.String("assets", "assets", "directory to write extracted textures into")
	flag.Parse()

	jarPaths := flag.Args()
	if len(jarPaths) == 0 {
		return errors.New("provide one or more JAR file paths as arguments")
	}
	if *dbURL == "" {
		return errors.New("DATABASE_URL not set and -db not provided")
	}

	ctx := context.Background()
	database, err := db.New(ctx, *dbURL)
	if err != nil {
		return fmt.Errorf("connect db: %w", err)
	}
	defer database.Close()

	imp := importer.New(database, *assetsDir)
	result, err := imp.Run(ctx, jarPaths)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(out))

	GoLog.Infof("done: recipes_imported=%d skipped=%d translations=%d tags=%d textures=%d warnings=%d",
		result.Recipes, result.RecipesSkip, result.Translations, result.Tags, result.Textures, len(result.Warnings))
	return nil
}
