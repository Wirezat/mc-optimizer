package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/production-optimizer/internal/api"
	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/logging"
	"github.com/Wirezat/production-optimizer/internal/plugins"
	"github.com/Wirezat/production-optimizer/internal/render"
	"github.com/Wirezat/production-optimizer/internal/service"
)

func main() {
	if err := run(); err != nil {
		GoLog.Errorf("fatal: %v", err)
		os.Exit(1) // only called after run() returns — defers inside run() have already executed
	}
}

func run() error {
	port := os.Getenv("SERVER_PORT")
	if port == "" {
		GoLog.Warn("SERVER_PORT is not set, defaulting to 8080")
		port = "8080"
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("DATABASE_URL is not set")
	}

	autoScaleMax := int64(10_000)
	if v := os.Getenv("AUTO_SCALE_MAX"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			autoScaleMax = n
		} else {
			GoLog.Warnf("AUTO_SCALE_MAX invalid, using default 500: %v", err)
		}
	}

	if err := GoLog.ToFile(); err != nil {
		GoLog.Warnf("file logging unavailable: %v", err)
	} else {
		logPath := GoLog.LogPath()
		if err := logging.Global.Load(logPath); err != nil {
			GoLog.Warnf("log store load: %v", err)
		}
		if err := logging.Global.Tail(logPath); err != nil {
			GoLog.Warnf("log store tail: %v", err)
		}
	}

	ctx := context.Background()
	database, err := db.New(ctx, dbURL)
	if err != nil {
		return err
	}
	defer database.Close()
	GoLog.Infof("Database: %s", maskPassword(dbURL))

	// ── Background cleanup ticker ─────────────────────────────────────────
	// Purges expired tokens and solver_drafts.
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			n, err := database.DeleteExpiredTokens(context.Background())
			if err != nil {
				GoLog.Errorf("cleanup: delete expired tokens: %v", err)
			} else if n > 0 {
				GoLog.Infof("cleanup: deleted expired tokens: count=%d", n)
			}
			d, err := database.DeleteExpiredSolverDrafts(context.Background())
			if err != nil {
				GoLog.Errorf("cleanup: delete expired solver drafts: %v", err)
			} else if d > 0 {
				GoLog.Infof("cleanup: deleted expired solver drafts: count=%d", d)
			}
		}
	}()

	// One compiled program per mod, shared by every request; the resolver
	// compiles into it lazily on the first cache miss for that mod.
	pluginRegistry := plugins.NewRegistry()
	variantResolver := service.NewVariantResolver(database, pluginRegistry)

	plSvc := service.NewPLService(database, autoScaleMax, variantResolver)

	mux := http.NewServeMux()

	// Health check — used by load balancers and monitoring
	mux.HandleFunc("GET /api/health", healthHandler)

	// Auth (no auth middleware)
	mux.HandleFunc("GET /api/auth/config", api.AuthConfigHandler(database))
	mux.HandleFunc("POST /api/auth/register", api.RegisterHandler(database))
	mux.HandleFunc("POST /api/auth/login", api.LoginHandler(database))
	mux.HandleFunc("POST /api/auth/refresh", api.RefreshHandler(database))
	mux.HandleFunc("POST /api/auth/logout", api.LogoutHandler(database))

	// Protected routes — wrap with RequireAuth middleware
	protected := api.RequireAuth(database)
	adminOnly := func(h http.Handler) http.Handler { return protected(api.RequireAdmin(h)) }
	ownerOnly := func(h http.Handler) http.Handler { return protected(api.RequireOwner(h)) }

	// Current user
	mux.Handle("GET /api/me", protected(api.MeHandler(database)))
	mux.Handle("PATCH /api/me/username", protected(api.ChangeUsernameHandler(database)))
	mux.Handle("PATCH /api/me/password", protected(api.ChangePasswordHandler(database)))

	// Log viewer
	mux.Handle("GET /api/logs", adminOnly(api.LogsHandler()))
	mux.Handle("GET /api/logs/stream", adminOnly(api.LogsStreamHandler()))

	// Admin settings
	mux.Handle("GET /api/admin/settings", adminOnly(api.GetAdminSettingsHandler(database)))
	mux.Handle("PATCH /api/admin/settings", adminOnly(api.UpdateAdminSettingsHandler(database)))

	// Admin user management
	mux.Handle("GET /api/admin/users", adminOnly(api.ListUsersHandler(database)))
	mux.Handle("PATCH /api/admin/users/{user_id}", adminOnly(api.UpdateUserHandler(database)))
	mux.Handle("DELETE /api/admin/users/{user_id}", adminOnly(api.DeleteUserHandler(database)))
	mux.Handle("PUT /api/admin/users/{user_id}/owner", ownerOnly(api.TransferOwnershipHandler(database)))

	mux.Handle("GET /api/saves", protected(api.ListSavesHandler(database)))
	mux.Handle("POST /api/saves", protected(api.CreateSaveHandler(database)))
	mux.Handle("GET /api/saves/{save_id}", protected(api.GetSaveHandler(database)))
	mux.Handle("DELETE /api/saves/{save_id}", protected(api.DeleteSaveHandler(database)))

	mux.Handle("GET /api/saves/{save_id}/active-mods", protected(api.GetActiveModsHandler(database)))
	mux.Handle("PUT /api/saves/{save_id}/active-mods", protected(api.SetActiveModsHandler(database)))
	mux.Handle("POST /api/saves/{save_id}/active-mods/dependents", protected(api.CheckActiveModDependentsHandler(database)))
	mux.Handle("GET /api/saves/{save_id}/factories", protected(api.ListFactoriesHandler(database)))
	mux.Handle("POST /api/saves/{save_id}/factories", protected(api.CreateFactoryHandler(database)))
	mux.Handle("GET /api/factories/{factory_id}", protected(api.GetFactoryHandler(database)))
	mux.Handle("PATCH /api/factories/{factory_id}", protected(api.UpdateFactoryHandler(database)))
	mux.Handle("DELETE /api/factories/{factory_id}", protected(api.DeleteFactoryHandler(database)))

	// PL groups (visual grouping / ordering only)
	mux.Handle("GET /api/factories/{factory_id}/pl-groups", protected(api.ListPLGroupsHandler(database)))
	mux.Handle("POST /api/factories/{factory_id}/pl-groups", protected(api.CreatePLGroupHandler(database)))
	mux.Handle("PATCH /api/pl-groups/{group_id}", protected(api.RenamePLGroupHandler(database)))
	mux.Handle("DELETE /api/pl-groups/{group_id}", protected(api.DeletePLGroupHandler(database)))
	mux.Handle("PATCH /api/pl-groups/{group_id}/position", protected(api.ReorderHandler(api.GroupPositionStore{DB: database}, "group_id")))

	// Source factory outputs (Quell-Modus)
	mux.Handle("GET /api/factories/{factory_id}/source-outputs", protected(api.ListFactorySourceOutputsHandler(database)))
	mux.Handle("POST /api/factories/{factory_id}/source-outputs", protected(api.UpsertFactorySourceOutputHandler(database)))
	mux.Handle("DELETE /api/source-outputs/{output_id}", protected(api.DeleteFactorySourceOutputHandler(database)))

	// Source factory inputs
	mux.Handle("GET /api/factories/{factory_id}/source-inputs", protected(api.ListFactorySourceInputsHandler(database)))
	mux.Handle("POST /api/factories/{factory_id}/source-inputs", protected(api.UpsertFactorySourceInputHandler(database)))
	mux.Handle("DELETE /api/source-inputs/{input_id}", protected(api.DeleteFactorySourceInputHandler(database)))

	mux.Handle("GET /api/mods", api.ListModsHandler(database)) // public: global game data, needed for /demo/solve pre-login
	mux.Handle("POST /api/mods", adminOnly(api.CreateModHandler(database)))
	mux.Handle("PUT /api/mods/{mod_id}", adminOnly(api.UpdateModHandler(database)))
	mux.Handle("GET /api/mods/{mod_id}/modrinth-preview", adminOnly(api.ModrinthPreviewHandler(database)))
	mux.Handle("DELETE /api/mods/{mod_id}", adminOnly(api.DeleteModHandler(database)))
	mux.Handle("GET /api/machines", protected(api.ListAllMachinesHandler(database, "assets")))
	mux.Handle("GET /api/mods/{mod_id}/machines", protected(api.ListMachinesHandler(database)))
	mux.Handle("PATCH /api/mods/{mod_id}/machines/{machine_id}", adminOnly(api.UpdateMachineHandler(database)))
	mux.Handle("GET /api/mods/{mod_id}/machines/{machine_id}/interfaces", protected(api.ListMachineInterfacesHandler(database)))
	mux.Handle("POST /api/mods/{mod_id}/machines/{machine_id}/interfaces", adminOnly(api.AddMachineInterfaceHandler(database)))
	mux.Handle("DELETE /api/mods/{mod_id}/machines/{machine_id}/interfaces/{base_mod_id}/{base_machine_id}", adminOnly(api.DeleteMachineInterfaceHandler(database)))
	mux.Handle("GET /api/mods/{mod_id}/machines/{machine_id}/slots", protected(api.ListMachineSlotsHandler(database)))
	mux.Handle("GET /api/mods/{mod_id}/items", protected(api.ListModItemsHandler(database, "assets")))
	mux.Handle("GET /api/mods/{mod_id}/fluids", protected(api.ListModFluidsHandler(database, "assets")))
	mux.Handle("PATCH /api/mods/{mod_id}/items/{item_id}", adminOnly(api.UpdateItemHandler(database)))
	mux.Handle("GET /api/recipes", protected(api.ListRecipesCatalogHandler(database)))
	mux.Handle("GET /api/mods/{mod_id}/recipes", protected(api.ListModRecipesHandler(database)))
	mux.Handle("POST /api/mods/{mod_id}/recipes", adminOnly(api.CreateModRecipeHandler(database)))
	mux.Handle("PATCH /api/recipes/{recipe_id}", adminOnly(api.UpdateRecipeNameHandler(database)))
	mux.Handle("DELETE /api/recipes/{recipe_id}", adminOnly(api.DeleteRecipeHandler(database)))

	mux.Handle("GET /api/items", api.SearchItemsHandler(database, "assets")) // public: global game data, needed for /demo/solve pre-login
	mux.Handle("GET /api/items/{mod_id}/{item_id}/recipes", protected(api.GetItemRecipesHandler(database)))
	mux.Handle("GET /api/fluids/{mod_id}/{fluid_id}/recipes", protected(api.GetFluidRecipesHandler(database)))
	mux.Handle("GET /api/fluids", api.SearchFluidsHandler(database, "assets")) // public: global game data, needed for /demo/solve pre-login
	mux.Handle("GET /api/tags", protected(api.SearchTagsHandler(database)))
	mux.Handle("GET /api/tag-members", protected(api.ListTagMembersHandler(database)))
	mux.Handle("GET /api/trades", protected(api.ListVillagerTradesHandler(database)))

	// Renders block models to icons on demand and caches the result; the import
	// handler drops the cache.
	renderCache := render.NewCache(render.NewLoader("assets"))

	mux.Handle("GET /api/import/status", adminOnly(api.ImportStatusHandler(database)))
	mux.Handle("POST /api/import/modfile", adminOnly(api.ImportModFileHandler(database, "assets", renderCache)))

	mux.Handle("PATCH /api/machine-groups/{group_id}/status", protected(api.UpdateMachineGroupStatusHandler(database)))
	mux.Handle("PUT /api/machine-groups/{group_id}/variant", protected(api.SetGroupVariantHandler(database, variantResolver)))

	mux.Handle("GET /plugin-assets/{mod_id}/plugin.js", protected(api.PluginAssetHandler(database)))
	mux.Handle("GET /api/saves/{save_id}/mod-config-defaults", protected(api.GetSaveModConfigDefaultsHandler(database)))
	mux.Handle("PUT /api/saves/{save_id}/mod-config-defaults/{mod_id}", protected(api.SetSaveModConfigDefaultHandler(database)))

	mux.Handle("POST /api/factories/{factory_id}/discover", protected(api.DiscoverHandler(database)))
	mux.Handle("POST /api/factories/{factory_id}/solve", protected(api.SolveHandler(database, plSvc)))
	mux.Handle("POST /api/demo/discover", api.DemoDiscoverHandler(database))                          // public: no factory/save behind this, safe to expose pre-login
	mux.Handle("POST /api/demo/solve", api.DemoSolveHandler(database, autoScaleMax, variantResolver)) // public: no factory/save behind this, safe to expose pre-login
	mux.Handle("POST /api/factories/{factory_id}/production-line/confirm", protected(api.ConfirmProductionLineHandler(database, plSvc)))
	mux.Handle("GET /api/factories/{factory_id}/production-lines", protected(api.ListProductionLinesHandler(database, variantResolver)))

	mux.Handle("GET /api/production-lines/{line_id}", protected(api.GetProductionLineHandler(database)))
	mux.Handle("PATCH /api/production-lines/{line_id}/status", protected(api.UpdateProductionLineStatusHandler(database)))
	mux.Handle("PATCH /api/production-lines/{line_id}/mark-built", protected(api.MarkProductionLineBuiltHandler(database)))
	mux.Handle("POST /api/production-lines/{line_id}/resolve", protected(api.ResolveProductionLineHandler(database, plSvc)))
	mux.Handle("PATCH /api/production-lines/{line_id}/position", protected(api.PLReorderHandler(database)))
	mux.Handle("PATCH /api/production-lines/{line_id}/group", protected(api.SetPLGroupHandler(database)))
	mux.Handle("DELETE /api/production-lines/{line_id}", protected(api.DeleteProductionLineHandler(database)))

	// Frontend pages — clean URLs without .html extension
	page := func(file string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// A page names the module and stylesheet URLs the browser then
			// fetches, so a stale one drags a whole stale deploy in behind it.
			// ServeFile still answers an unchanged file with a 304.
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, "web/pages/"+file)
		}
	}
	mux.HandleFunc("GET /personal", page("personal.html"))
	mux.HandleFunc("GET /login", page("login.html"))
	mux.HandleFunc("GET /saves", page("saves.html"))
	mux.HandleFunc("GET /saves/{save_id}", page("save.html"))
	mux.HandleFunc("GET /factories/{factory_id}", page("factory.html"))
	mux.HandleFunc("GET /factories/{factory_id}/solve", page("solve.html"))
	mux.HandleFunc("GET /demo/solve", page("solve.html")) // same file — see demo-mode branch inside solve.html
	mux.HandleFunc("GET /production-lines/{line_id}", page("production-line.html"))
	mux.HandleFunc("GET /catalog/mods", page("catalog-mods.html"))
	mux.HandleFunc("GET /catalog/items", page("catalog-items.html"))
	mux.HandleFunc("GET /catalog/fluids", page("catalog-fluids.html"))
	mux.HandleFunc("GET /catalog/machines", page("catalog-machines.html"))
	mux.HandleFunc("GET /catalog/trades", page("catalog-trades.html"))
	mux.HandleFunc("GET /admin/settings", page("admin-settings.html"))
	mux.HandleFunc("GET /admin/import", page("admin-import.html"))

	// Static assets and fallback.
	//
	// None of these URLs carry a content hash, so a file's URL stays the same
	// when the file changes. Without an explicit policy a browser falls back to
	// heuristic freshness and happily serves the previous deploy's JS — which it
	// did, repeatedly. Everything below therefore revalidates; the file servers
	// answer an unchanged file with a 304 off Last-Modified, so revalidation is
	// cheap and correctness does not depend on guessing a lifetime.
	staticFS := http.FileServer(http.Dir("web/static"))
	mux.Handle("/static/", http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "locales/") {
			// Translations are read on every page load and must never lag a
			// deploy, not even by one conditional request.
			w.Header().Set("Cache-Control", "no-store")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		staticFS.ServeHTTP(w, r)
	})))
	// Textures and models change only on import, but a re-import replaces them
	// at the same URL. A short lifetime keeps a page full of icons from
	// revalidating each one while bounding how long a stale icon can survive.
	// Rendered model icons, registered before the plain asset tree so the more
	// specific prefix wins. Nothing is written to disk — see internal/render.
	mux.Handle("GET /assets/render/", api.RenderModelHandler(renderCache))
	assetFS := http.FileServer(http.Dir("assets"))
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")
		assetFS.ServeHTTP(w, r)
	})))
	mux.Handle("GET /{$}", http.RedirectHandler("/login", http.StatusFound))
	// Pages are the entry point to everything above; a stale one pulls in stale
	// module URLs, so it revalidates too.
	pagesFS := http.FileServer(http.Dir("web/pages"))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		pagesFS.ServeHTTP(w, r)
	}))

	srv := &http.Server{
		Addr:           ":" + port,
		Handler:        mux,
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1 MiB — protect against header abuse
	}

	// Capture SIGINT / SIGTERM for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	serverErr := make(chan error, 1)
	go func() {
		GoLog.Infof("Server listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("server error: %w", err)
	case sig := <-quit:
		GoLog.Infof("Received signal %s — shutting down gracefully", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			return fmt.Errorf("graceful shutdown failed: %w", err)
		}
		GoLog.Info("Server stopped cleanly")
	}

	return nil
}

// healthHandler responds with the current server time.
// GET /api/health → 200 {"status":"ok","time":"..."}
func healthHandler(w http.ResponseWriter, r *http.Request) {
	type response struct {
		Status string `json:"status"`
		Time   string `json:"time"`
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response{
		Status: "ok",
		Time:   time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		GoLog.Warnf("health encode error: %v", err)
	}
}

// maskPassword redacts the password in a Postgres DSN for safe logging.
// Falls back to the raw URL if parsing fails.
func maskPassword(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return "<unparseable DSN>"
	}
	if _, hasPassword := u.User.Password(); hasPassword {
		u.User = url.UserPassword(u.User.Username(), "***")
	}
	// Show only host:port, not the full DSN
	return net.JoinHostPort(u.Hostname(), u.Port()) + u.Path
}
