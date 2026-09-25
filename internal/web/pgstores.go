package web

import (
	"kestrix/internal/domain"
	"kestrix/internal/store"
)

// newPGStores is the external PostgreSQL adapter. Its successful connection
// path belongs to integration coverage; unit tests replace this seam with
// in-memory stores while still covering initStores orchestration.
var newPGStores = func(databaseURL, defaultMode string) (domain.ProjectStore, domain.RunStore, error) {
	pg, err := store.NewPG(databaseURL, defaultMode)
	if err != nil {
		return nil, nil, err
	}
	return pg, pg, nil
}
