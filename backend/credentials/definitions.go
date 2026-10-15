package credentials

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// DefinitionsJSON is the single source of truth for the credentials: one
// credential per E1 top-level group, with one attribute per E1 category code.
// scheme/generate.py reads the same file, and the frontend gets it from
// GET /api/definitions.
//
//go:embed definitions.json
var DefinitionsJSON []byte

type Translated struct {
	En string `json:"en"`
	Nl string `json:"nl"`
}

type AttributeDefinition struct {
	Id string `json:"id"`
	// E1 is the E1 category code summed into this attribute; empty for
	// attributes that don't come from categorized transactions.
	E1          string     `json:"e1,omitempty"`
	Name        Translated `json:"name"`
	Description Translated `json:"description"`
}

type CredentialDefinition struct {
	Key string `json:"key"`
	// E1Group is the E1 top-level group, e.g. "income" or "expense.essential".
	E1Group     string                `json:"e1_group,omitempty"`
	Name        Translated            `json:"name"`
	Description Translated            `json:"description"`
	Attributes  []AttributeDefinition `json:"attributes"`
}

type Definitions struct {
	PeriodAttributes []AttributeDefinition  `json:"period_attributes"`
	Credentials      []CredentialDefinition `json:"credentials"`
}

// Defs holds the parsed DefinitionsJSON.
var Defs = mustParseDefinitions(DefinitionsJSON)

// AllKeys lists every credential this package can produce, in display order.
var AllKeys = func() []string {
	keys := make([]string, 0, len(Defs.Credentials))
	for _, c := range Defs.Credentials {
		keys = append(keys, c.Key)
	}
	return keys
}()

func mustParseDefinitions(data []byte) Definitions {
	var defs Definitions
	if err := json.Unmarshal(data, &defs); err != nil {
		panic(fmt.Sprintf("credentials/definitions.json: %v", err))
	}
	return defs
}
