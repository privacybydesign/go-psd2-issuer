package main

import (
	"crypto/rsa"
	"fmt"
	"os"
	"psd2-issuer/credentials"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/privacybydesign/irmago/irma"
)

type JwtCreator interface {
	// CreateJwt returns a signed issuance request for the given credentials.
	CreateJwt(creds []credentials.Credential) (jwt string, err error)
}

func NewIrmaJwtCreator(privateKeyPath string,
	issuerId string,
	credentialIds map[string]string,
	validity time.Duration,
) (*DefaultJwtCreator, error) {
	keyBytes, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, err
	}

	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM(keyBytes)
	if err != nil {
		return nil, err
	}

	return &DefaultJwtCreator{
		issuerId:      issuerId,
		privateKey:    privateKey,
		credentialIds: credentialIds,
		validity:      validity,
		now:           time.Now,
	}, nil
}

type DefaultJwtCreator struct {
	privateKey *rsa.PrivateKey
	issuerId   string
	// credentialIds maps a credential key (e.g. "income") to the full Yivi
	// credential type id (e.g. "irma-demo.enablenow.income").
	credentialIds map[string]string
	validity      time.Duration
	now           func() time.Time
}

func (jc *DefaultJwtCreator) CreateJwt(creds []credentials.Credential) (string, error) {
	if len(creds) == 0 {
		return "", fmt.Errorf("no credentials to issue")
	}

	// Financial data goes stale quickly, so the credentials expire much
	// sooner than the Yivi default.
	validity := irma.Timestamp(jc.now().Add(jc.validity))

	requests := make([]*irma.CredentialRequest, 0, len(creds))
	for _, cred := range creds {
		id, ok := jc.credentialIds[cred.Key]
		if !ok {
			return "", fmt.Errorf("no credential id configured for %q", cred.Key)
		}
		requests = append(requests, &irma.CredentialRequest{
			CredentialTypeID: irma.NewCredentialTypeIdentifier(id),
			Attributes:       cred.Attributes,
			Validity:         &validity,
			SdJwtBatchSize:   200,
		})
	}

	return irma.SignSessionRequest(
		irma.NewIssuanceRequest(requests),
		jwt.GetSigningMethod(jwt.SigningMethodRS256.Alg()),
		jc.privateKey,
		jc.issuerId,
	)
}
