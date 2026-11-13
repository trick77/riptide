package store

import (
	"path/filepath"
	"testing"

	"github.com/trick77/ship/kit/schemadoc"
)

// TestSchemaDoc keeps docs/schema.md in step with the migrations and the
// model.go row types. The test schema is the connection's search_path, so
// current_schema() is the freshly migrated one, never public.
func TestSchemaDoc(t *testing.T) {
	s, _ := testStore(t)
	sc, err := schemadoc.Describe(t.Context(), s.sqlDB, schemadoc.Postgres)
	if err != nil {
		t.Fatal(err)
	}
	es, err := schemadoc.Entities(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := schemadoc.Render(sc, es)
	if err != nil {
		t.Fatal(err)
	}
	schemadoc.Golden(t, doc, filepath.Join("..", "..", "..", "docs", "schema.md"))
}
