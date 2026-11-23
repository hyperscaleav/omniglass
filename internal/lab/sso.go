package lab

import (
	"encoding/base64"
	"fmt"
)

// A script's throwaway lab can run an identity provider (#2362): Dex, with
// one static client (the lab's server) and one static demo person, so a
// walkthrough signs in with single sign-on for real, through the provider's
// own login form. Every value here is a lab's demo value, published in this
// repository: the client secret and the password open nothing but a
// throwaway lab's Dex on its own Docker network.
//
// Only a throwaway lab runs it: the issuer is Dex's address on the lab
// network, which the server and a recorder's browser (on that network) reach
// and a learner's browser on the host does not.
const (
	// DexImage is the identity provider the lab runs, the one the SSO smoke
	// test runs (internal/api/oidc_dex_e2e_test.go).
	DexImage = "ghcr.io/dexidp/dex:v2.44.0"
	// DexService is its service name, so its address on the lab network.
	DexService = "dex"
	// DexPort is where it listens.
	DexPort = 5556
	// DexClientID and DexClientSecret are the lab server's registration at
	// Dex: demo values.
	DexClientID     = "omniglass"
	DexClientSecret = "lab-demo-dex-client-secret"
	// SSOClientSecret is the platform oauth2-client secret the seed makes
	// from them, which the sso settings name.
	SSOClientSecret = "lab-sso-client"
	// The demo person: an Omniglass person (a viewer) whose identity at Dex
	// is linked, and Dex's static password for them, its bcrypt hash below.
	SSODemoUser     = "avery"
	SSODemoLabel    = "Avery Quinn"
	SSODemoEmail    = "avery@example.com"
	SSODemoPassword = "lab-demo-password"
	SSODemoRole     = "viewer"
	// SSODemoUserID is the demo person's user id at Dex, the seed of the
	// subject Dex vouches for (DexSubject).
	SSODemoUserID = "6a1b2c3d-0000-4000-8000-0000000023a2"
	// ssoDemoPasswordHash is SSODemoPassword under bcrypt (cost 10), as Dex
	// reads a static password; a test holds the two together.
	ssoDemoPasswordHash = "$2a$10$RYtQR78MEw1Cxkl0Mmsxt.OKmlj5HJEAHNWWmAVFWPeyYna4f3EVO"
	// dexConnector is the connector Dex signs a static password in through.
	dexConnector = "local"
)

// DexIssuer is the lab's Dex on the lab network: the issuer the sso settings
// name and Dex's id_tokens carry.
const DexIssuer = "http://dex:5556/dex"

// DexSubject is the subject Dex vouches for a static password user by: its
// id and the connector, as Dex encodes them (a protobuf of the two fields,
// base64url without padding). Pure.
func DexSubject(userID, connector string) string {
	b := []byte{0x0a, byte(len(userID))}
	b = append(b, userID...)
	b = append(b, 0x12, byte(len(connector)))
	b = append(b, connector...)
	return base64.RawURLEncoding.EncodeToString(b)
}

// SSODemoIdentity is the demo person's identity at the lab's Dex, iss|sub:
// what the seed links.
func SSODemoIdentity() string { return DexDemoIdentity(DexIssuer) }

// DexDemoIdentity is the demo person's identity at a Dex run from DexConfig
// on issuer: the SSO smoke test signs in through one at its own address and
// holds the identity it logs to this.
func DexDemoIdentity(issuer string) string {
	return issuer + "|" + DexSubject(SSODemoUserID, dexConnector)
}

// DexConfig is Dex's configuration for issuer, with the lab server's client
// redirected to redirectURI and the demo person's static password. Pure; the
// SSO smoke test runs the same configuration on its own issuer.
func DexConfig(issuer, redirectURI string) string {
	return fmt.Sprintf(`issuer: %s
storage: {type: memory}
web: {http: 0.0.0.0:%d}
oauth2: {skipApprovalScreen: true}
enablePasswordDB: true
staticPasswords:
  - email: %s
    hash: %q
    username: %s
    userID: %s
staticClients:
  - id: %s
    secret: %s
    name: Omniglass
    redirectURIs: [%q]
`, issuer, DexPort, SSODemoEmail, ssoDemoPasswordHash, SSODemoUser, SSODemoUserID, DexClientID, DexClientSecret, redirectURI)
}

// dexConfigEnv carries Dex's configuration into its container, which writes
// it where Dex reads it: the lab mounts no host file.
const dexConfigEnv = "DEX_CONFIG"
