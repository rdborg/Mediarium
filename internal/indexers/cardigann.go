package indexers

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Definition is the minimal slice of a Cardigann YAML indexer definition
// this engine actually reads: identity and category metadata, used to
// register a known indexer and label its results. See the package doc
// comment in newznab.go for why the full Cardigann template/selector spec
// isn't implemented — actual searches run through NewznabClient.
type Definition struct {
	ID    string   `yaml:"id"`
	Name  string   `yaml:"name"`
	Type  string   `yaml:"type"` // public|private|semi-private
	Links []string `yaml:"links"`
	Caps  struct {
		Categories map[string]string `yaml:"categories"`
	} `yaml:"caps"`
}

// ParseDefinition reads a Cardigann YAML indexer definition (as found in
// the community-maintained Prowlarr/Indexers repo referenced by PRD §4.4).
func ParseDefinition(data []byte) (*Definition, error) {
	var def Definition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parse cardigann definition: %w", err)
	}
	if def.ID == "" {
		return nil, fmt.Errorf("cardigann definition missing required 'id' field")
	}
	return &def, nil
}
