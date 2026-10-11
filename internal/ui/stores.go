package ui

import (
	"github.com/AlexK-Notable/herdr-panel/internal/store"
	"github.com/AlexK-Notable/herdr-panel/spec"
)

// NewStore is the store a spec's store definition describes, on the file at
// path. Everything that builds a panel's stores (the panel, paneltest,
// observe --sandbox) goes through it, so they cannot differ.
func NewStore(s *spec.Spec, sd *spec.Store, path string) store.Store {
	if sd.Kind == spec.TOMLFile {
		// A key is written as the TOML type its field edits.
		kinds := map[string]store.Kind{}
		for _, f := range s.Fields() {
			if f.Store != sd.Name {
				continue
			}
			switch f.Type {
			case spec.Bool:
				kinds[f.Key] = store.KindBool
			case spec.Int, spec.Duration:
				kinds[f.Key] = store.KindInt
			case spec.Choice, spec.Text, spec.Secret:
				kinds[f.Key] = store.KindString
			}
		}
		return &store.TOMLFile{Path: path, Create: sd.Create, Kinds: kinds}
	}
	return &store.EnvFile{Path: path, Create: sd.Create}
}

// StoreExt is the file extension a store kind's files have, for copies.
func StoreExt(k spec.StoreKind) string {
	if k == spec.TOMLFile {
		return ".toml"
	}
	return ".env"
}
