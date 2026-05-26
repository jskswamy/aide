package explain

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed recipes/*.md
var recipeFS embed.FS

// LoadRecipes reads the embedded recipe markdown files. The Topic is the file
// base name (without .md); the Title is the first markdown heading.
func LoadRecipes() ([]Recipe, error) {
	entries, err := recipeFS.ReadDir("recipes")
	if err != nil {
		return nil, fmt.Errorf("reading embedded recipes: %w", err)
	}
	var recipes []Recipe
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		data, err := recipeFS.ReadFile("recipes/" + name)
		if err != nil {
			return nil, fmt.Errorf("reading recipe %s: %w", name, err)
		}
		body := string(data)
		recipes = append(recipes, Recipe{
			Topic: strings.TrimSuffix(name, ".md"),
			Title: firstHeading(body),
			Body:  body,
		})
	}
	sort.Slice(recipes, func(i, j int) bool { return recipes[i].Topic < recipes[j].Topic })
	return recipes, nil
}

func firstHeading(md string) string {
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}
