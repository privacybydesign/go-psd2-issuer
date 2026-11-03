package main

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"psd2-issuer/credentials"

	"github.com/stretchr/testify/require"
)

// issuanceRequestJson returns the issuance request inside a signed JWT as raw
// JSON, so tests can check which fields are sent to the IRMA server.
func issuanceRequestJson(t *testing.T, token string) map[string]any {
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)

	var claims struct {
		Request struct {
			Request map[string]any `json:"request"`
		} `json:"iprequest"`
	}
	require.NoError(t, json.Unmarshal(payload, &claims))
	return claims.Request.Request
}

func TestCreateJwtSdJwtBatchSize(t *testing.T) {
	ids := map[string]string{"income": "pbdf-staging.pbdf.income"}
	creds := []credentials.Credential{{Key: "income", Attributes: map[string]string{"salary": "100"}}}

	t.Run("left out when 0", func(t *testing.T) {
		jc, err := NewIrmaJwtCreator("../test-secrets/priv.pem", "psd2_issuer", ids, time.Hour, 0)
		require.NoError(t, err)
		token, err := jc.CreateJwt(creds)
		require.NoError(t, err)

		request := issuanceRequestJson(t, token)
		credentialRequests := request["credentials"].([]any)
		require.Len(t, credentialRequests, 1)
		require.NotContains(t, credentialRequests[0], "sdJwtBatchSize")
	})

	t.Run("sent when set", func(t *testing.T) {
		jc, err := NewIrmaJwtCreator("../test-secrets/priv.pem", "psd2_issuer", ids, time.Hour, 5)
		require.NoError(t, err)
		token, err := jc.CreateJwt(creds)
		require.NoError(t, err)

		request := issuanceRequestJson(t, token)
		credentialRequests := request["credentials"].([]any)
		require.Len(t, credentialRequests, 1)
		require.Equal(t, float64(5), credentialRequests[0].(map[string]any)["sdJwtBatchSize"])
	})
}
