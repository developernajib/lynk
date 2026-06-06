// Package jwt issues and verifies RS256 JSON Web Tokens.
//
// RS256 over a shared HMAC secret: only the identity-owning service holds the
// private key and can sign, while the gateway and sibling services hold only
// the public key and can verify. A leaked public key cannot mint tokens.
//
// One Config feeds both NewSigner and NewVerifier so the issuer claim can
// never drift between the service that signs and the services that verify;
// an iss mismatch makes every verification fail silently and is miserable to
// debug (found live in production once, hence this design).
package jwt

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"

	"github.com/developernajib/lynk/services/core/internal/platform/secure"
)

// Config holds the token settings for both sides. A verifier-only service
// leaves PrivateKeyPEM empty.
type Config struct {
	// PrivateKeyPEM is the RS256 signing key. Only the token issuer sets it.
	PrivateKeyPEM string
	// PublicKeyPEM verifies tokens; every service holds it.
	PublicKeyPEM string
	// Issuer is the iss claim, identical across signer and all verifiers.
	Issuer string
	// AccessTokenTTL is the short life of a stateless access token.
	AccessTokenTTL time.Duration
	// RefreshTokenTTL is read by the application layer for its opaque,
	// stored refresh tokens; refresh tokens are not JWTs in this design.
	RefreshTokenTTL time.Duration
}

// TokenType distinguishes the principal kinds on the platform.
type TokenType string

const (
	TokenTypeUser       TokenType = "user"       // tenant staff
	TokenTypeCustomer   TokenType = "customer"   // global customer, no tenant
	TokenTypeSuperAdmin TokenType = "superadmin" // platform operator
)

// Claims is the token payload. Embedding RegisteredClaims provides the
// standard fields (exp, iat, jti, sub) and their validation.
type Claims struct {
	jwtlib.RegisteredClaims
	TenantID  string    `json:"tenant_id,omitempty"`
	BranchID  string    `json:"branch_id,omitempty"`
	Role      string    `json:"role,omitempty"`
	TokenType TokenType `json:"token_type"`
}

// ErrInvalidToken is the single sentinel for any verification failure. The
// reason (expired vs bad signature vs malformed) is deliberately not exposed:
// it helps an attacker probe tokens. Detail belongs in internal logs only.
var ErrInvalidToken = errors.New("invalid token")

// Signer holds the private key and mints access tokens.
type Signer struct {
	privateKey     *rsa.PrivateKey
	issuer         string
	accessTokenTTL time.Duration
}

// NewSigner parses the PEM private key from cfg and returns a ready Signer.
func NewSigner(cfg Config) (*Signer, error) {
	privateKey, err := jwtlib.ParseRSAPrivateKeyFromPEM([]byte(cfg.PrivateKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("jwt: parse private key: %w", err)
	}
	return &Signer{privateKey: privateKey, issuer: cfg.Issuer, accessTokenTTL: cfg.AccessTokenTTL}, nil
}

// IssuedToken is a signed access token plus what callers need afterward: the
// jti to revoke it via the blacklist on logout, and the expiry to TTL the
// blacklist entry so it cleans itself up when the token would die anyway.
type IssuedToken struct {
	Token     string
	ID        string // jti
	ExpiresAt time.Time
}

// IssueAccessToken builds and signs a short-lived RS256 access token.
func (s *Signer) IssueAccessToken(subject string, claims Claims) (IssuedToken, error) {
	// A unique, unguessable token id makes per-token revocation possible.
	tokenID, err := secure.HexToken(16)
	if err != nil {
		return IssuedToken{}, err
	}

	now := time.Now()
	expiresAt := now.Add(s.accessTokenTTL)
	claims.RegisteredClaims = jwtlib.RegisteredClaims{
		Subject:   subject,
		Issuer:    s.issuer,
		ID:        tokenID,
		IssuedAt:  jwtlib.NewNumericDate(now),
		ExpiresAt: jwtlib.NewNumericDate(expiresAt),
		NotBefore: jwtlib.NewNumericDate(now),
	}

	token := jwtlib.NewWithClaims(jwtlib.SigningMethodRS256, &claims)
	signed, err := token.SignedString(s.privateKey)
	if err != nil {
		return IssuedToken{}, fmt.Errorf("jwt: sign: %w", err)
	}
	return IssuedToken{Token: signed, ID: tokenID, ExpiresAt: expiresAt}, nil
}

// Verifier holds only the public key and validates tokens.
type Verifier struct {
	publicKey *rsa.PublicKey
	issuer    string
}

// NewVerifier parses the PEM public key from cfg and returns a ready
// Verifier. No private key required.
func NewVerifier(cfg Config) (*Verifier, error) {
	publicKey, err := jwtlib.ParseRSAPublicKeyFromPEM([]byte(cfg.PublicKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("jwt: parse public key: %w", err)
	}
	return &Verifier{publicKey: publicKey, issuer: cfg.Issuer}, nil
}

// Verify parses and validates a token string, returning its claims.
//
// The algorithm is pinned to RS256: the classic JWT attack sends alg=none or
// alg=HS256 so the server verifies an attacker-chosen MAC against the public
// key. Any non-RSA method is rejected.
func (v *Verifier) Verify(tokenString string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwtlib.ParseWithClaims(tokenString, claims, func(token *jwtlib.Token) (any, error) {
		if _, ok := token.Method.(*jwtlib.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return v.publicKey, nil
	},
		jwtlib.WithValidMethods([]string{"RS256"}),
		jwtlib.WithIssuer(v.issuer),
		jwtlib.WithExpirationRequired(),
	)
	if err != nil {
		// Collapse every failure into the opaque sentinel.
		return nil, ErrInvalidToken
	}
	return claims, nil
}
