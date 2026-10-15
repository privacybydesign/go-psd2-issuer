package main

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"psd2-issuer/credentials"
	"psd2-issuer/enablenow"

	"github.com/stretchr/testify/require"
)

type issueSpecification struct {
	CredentialID string `xml:"CredentialID"`
	Attributes   []struct {
		Id string `xml:"id,attr"`
	} `xml:"Attributes>Attribute"`
}

// TestSchemeMatchesCredentials checks that the proposed scheme in ../scheme
// declares exactly the attributes the issuer computes, so issuance can't fail
// on a missing or unknown attribute.
func TestSchemeMatchesCredentials(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	accounts := []enablenow.Account{{Iban: "NL91ABNA0417164300", AccountHolder: "J. Jansen"}}
	computed := credentials.Compute(accounts, nil, "bank", credentials.Options{Now: now})

	for _, cred := range computed {
		path := filepath.Join("..", "scheme", "irma-demo", "enablenow", "Issues", cred.Key, "description.xml")
		content, err := os.ReadFile(path)
		require.NoError(t, err)

		var spec issueSpecification
		require.NoError(t, xml.Unmarshal(content, &spec))
		require.Equal(t, cred.Key, spec.CredentialID)

		var declared []string
		for _, a := range spec.Attributes {
			declared = append(declared, a.Id)
		}
		var issued []string
		for name := range cred.Attributes {
			issued = append(issued, name)
		}
		slices.Sort(declared)
		slices.Sort(issued)
		require.Equal(t, declared, issued, "attributes of %s", cred.Key)
	}
}
