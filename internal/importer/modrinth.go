package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wirezat/production-optimizer/internal/model"
)

const modrinthBase = "https://api.modrinth.com/v2"

var modrinthClient = &http.Client{Timeout: 10 * time.Second}

type modrinthProject struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Slug        string `json:"slug"`
	SourceURL   string `json:"source_url"`
	WikiURL     string `json:"wiki_url"`
	IssuesURL   string `json:"issues_url"`
	DiscordURL  string `json:"discord_url"`
	Team        string `json:"team"`
	License     struct {
		ID string `json:"id"`
	} `json:"license"`
}

type modrinthMember struct {
	Role string `json:"role"`
	User struct {
		Username string `json:"username"`
	} `json:"user"`
}

type modrinthSearchHit struct {
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Author string `json:"author"`
}

func modrinthGet(path string, out any) error {
	resp, err := modrinthClient.Get(modrinthBase + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil // caller checks nil pointer
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("modrinth %s: status %d", path, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

// lookupProject tries to find a Modrinth project for modID/displayName.
// Returns nil if nothing found.
func lookupProject(modID, displayName string) (*modrinthProject, error) {
	// 1. Direct slug lookup by mod ID
	var p modrinthProject
	if err := modrinthGet("/project/"+url.PathEscape(modID), &p); err != nil {
		return nil, err
	}
	if p.Slug != "" {
		return &p, nil
	}

	// 2. Search by display name, exact title match
	if displayName == "" || displayName == modID {
		return nil, nil
	}
	searchURL := fmt.Sprintf("/search?query=%s&limit=5", url.QueryEscape(displayName))
	var result struct {
		Hits []modrinthSearchHit `json:"hits"`
	}
	if err := modrinthGet(searchURL, &result); err != nil {
		return nil, err
	}
	var slug string
	for _, h := range result.Hits {
		if strings.EqualFold(h.Title, displayName) {
			slug = h.Slug
			break
		}
	}
	if slug == "" {
		return nil, nil
	}

	var p2 modrinthProject
	if err := modrinthGet("/project/"+slug, &p2); err != nil {
		return nil, err
	}
	if p2.Slug == "" {
		return nil, nil
	}
	return &p2, nil
}

func teamOwner(teamID string) string {
	if teamID == "" {
		return ""
	}
	var members []modrinthMember
	if err := modrinthGet("/team/"+teamID+"/members", &members); err != nil {
		return ""
	}
	for _, m := range members {
		if strings.EqualFold(m.Role, "Owner") {
			return m.User.Username
		}
	}
	if len(members) > 0 {
		return members[0].User.Username
	}
	return ""
}

// FetchModrinthMetadata fetches Modrinth metadata for a single mod without saving.
// If slugOverride is non-empty it is tried first, then falls back to the normal
// lookup-by-modID / search-by-displayName strategy.
func FetchModrinthMetadata(modID, displayName, slugOverride string) (*model.ModMetadata, error) {
	var proj *modrinthProject
	var err error

	if slugOverride != "" {
		var p modrinthProject
		if err = modrinthGet("/project/"+url.PathEscape(slugOverride), &p); err != nil {
			return nil, err
		}
		if p.Slug != "" {
			proj = &p
		}
	}

	if proj == nil {
		proj, err = lookupProject(modID, displayName)
		if err != nil {
			return nil, err
		}
	}
	if proj == nil {
		return nil, nil
	}

	author := teamOwner(proj.Team)
	meta := &model.ModMetadata{
		ModID:        modID,
		Description:  proj.Description,
		Author:       author,
		License:      proj.License.ID,
		URLSource:    proj.SourceURL,
		URLModrinth:  fmt.Sprintf("https://modrinth.com/mod/%s", proj.Slug),
		URLWiki:      proj.WikiURL,
		URLIssues:    proj.IssuesURL,
		URLDiscord:   proj.DiscordURL,
		ModrinthSlug: proj.Slug,
	}
	return meta, nil
}

// EnrichModsFromModrinth fetches Modrinth metadata for each mod and stores it.
// modNames maps mod_id → display name (may be empty if not found in JAR).
// Errors per-mod are logged as warnings, not fatal.
func (imp *Importer) EnrichModsFromModrinth(ctx context.Context, modNames map[string]string) {
	for modID, displayName := range modNames {
		proj, err := lookupProject(modID, displayName)
		if err != nil {
			log.Printf("modrinth: lookup %s: %v", modID, err)
			continue
		}
		if proj == nil {
			continue
		}

		author := teamOwner(proj.Team)

		meta := model.ModMetadata{
			ModID:        modID,
			Description:  proj.Description,
			Author:       author,
			License:      proj.License.ID,
			URLSource:    proj.SourceURL,
			URLModrinth:  fmt.Sprintf("https://modrinth.com/mod/%s", proj.Slug),
			URLWiki:      proj.WikiURL,
			URLIssues:    proj.IssuesURL,
			URLDiscord:   proj.DiscordURL,
			ModrinthSlug: proj.Slug,
		}

		if err := imp.db.UpdateModMetadata(ctx, meta); err != nil {
			log.Printf("modrinth: save metadata %s: %v", modID, err)
		}
	}
}
