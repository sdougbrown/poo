package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"time"
)

// The proof a key-bound grant carries (RFC 9449).
//
// A provider that binds its refresh token to a key pair makes every token
// request carry a proof signed with the private half. The pair is created when
// the person signs in, so it belongs to this machine, and the credential keeps
// it: the grant is useless without it, and a refresh that cannot sign is a
// sign-in again.
//
// The key is P-256 and the signature is ES256, because that is the one the
// token endpoints of this kind take. The proof's public half travels in its
// header, so the endpoint can check the signature without having seen the key
// before.

// DPoPKey is a P-256 key pair as the credential stores it and as the proof's
// header carries its public half.
//
// The field names are the JWK ones, and the credential keeps this shape rather
// than an encoding of its own: the payload is what the endpoint reads.
type DPoPKey struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	D   string `json:"d"`
}

// newDPoPKey creates the key pair a sign-in binds its tokens to.
func newDPoPKey() (DPoPKey, error) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return DPoPKey{}, fmt.Errorf("auth: creating the proof key: %w", err)
	}
	return jwkOf(private), nil
}

// jwkOf renders a key pair as the credential stores it.
//
// The coordinates are the fixed-width big-endian numbers JWS uses, which is
// what ecdsa's own values are not: the leading zero bytes of a coordinate are
// part of the number on the wire.
func jwkOf(private *ecdsa.PrivateKey) DPoPKey {
	return DPoPKey{
		Kty: "EC",
		Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString(padded(private.X)),
		Y:   base64.RawURLEncoding.EncodeToString(padded(private.Y)),
		D:   base64.RawURLEncoding.EncodeToString(padded(private.D)),
	}
}

// padded writes a coordinate as the 32 bytes P-256 uses.
func padded(value *big.Int) []byte {
	out := make([]byte, 32)
	value.FillBytes(out)
	return out
}

// parseDPoPKey reads the key a credential holds.
//
// The file is one an operator may have edited, so the shape is checked here
// rather than trusted: a key that does not parse is a credential this machine
// cannot use, and the sign-in is what fixes it.
func parseDPoPKey(key *DPoPKey) (*ecdsa.PrivateKey, error) {
	if key == nil || key.Kty != "EC" || key.Crv != "P-256" {
		return nil, fmt.Errorf("%w: the credential holds no P-256 proof key", ErrRefresh)
	}
	x, err := decodeCoordinate(key.X)
	if err != nil {
		return nil, err
	}
	y, err := decodeCoordinate(key.Y)
	if err != nil {
		return nil, err
	}
	d, err := decodeCoordinate(key.D)
	if err != nil {
		return nil, err
	}
	private := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y},
		D:         d,
	}
	if !elliptic.P256().IsOnCurve(x, y) {
		return nil, fmt.Errorf("%w: the credential's proof key is not on the curve", ErrRefresh)
	}
	return private, nil
}

// decodeCoordinate reads one coordinate of the stored key.
func decodeCoordinate(value string) (*big.Int, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 32 {
		return nil, fmt.Errorf("%w: the credential's proof key does not decode", ErrRefresh)
	}
	return new(big.Int).SetBytes(raw), nil
}

// dpopProof is what one proof signs.
type dpopProof struct {
	// JTI is the proof's own id.
	JTI string
	// HTU is the URL the proof is for.
	HTU string
	// Nonce is the value the endpoint asked for, when it asked for one.
	Nonce string
	// IAT is when the proof was made, in Unix seconds. Zero takes the clock
	// the caller hands in, which is this machine's until a retry dates the
	// proof with the endpoint's instead.
	IAT int64
}

// sign renders the proof as the compact JWS the endpoint reads.
//
// The signature is the fixed-width R||S pair ECDSA produces, not the DER
// encoding some libraries return: the endpoint checks the JOSE form, and a DER
// signature is refused.
func (p dpopProof) sign(private *ecdsa.PrivateKey, now func() time.Time) (string, error) {
	iat := p.IAT
	if iat == 0 {
		iat = now().Unix()
	}
	header, err := json.Marshal(map[string]any{
		"typ": "dpop+jwt",
		"alg": "ES256",
		"jwk": map[string]string{
			"kty": "EC",
			"crv": "P-256",
			"x":   base64.RawURLEncoding.EncodeToString(padded(private.X)),
			"y":   base64.RawURLEncoding.EncodeToString(padded(private.Y)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("auth: encoding the proof's header: %w", err)
	}
	claims := map[string]any{
		"jti": p.JTI,
		"htm": "POST",
		"htu": p.HTU,
		"iat": iat,
	}
	if p.Nonce != "" {
		claims["nonce"] = p.Nonce
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("auth: encoding the proof's claims: %w", err)
	}
	signing := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, private, digest[:])
	if err != nil {
		return "", fmt.Errorf("auth: signing the proof: %w", err)
	}
	signature := append(padded(r), padded(s)...)
	return signing + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}
