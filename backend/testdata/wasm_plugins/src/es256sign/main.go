// es256sign is a disposable feasibility spike, NOT a real plugin. It exists
// only to answer one question empirically: can TinyGo 0.41.1, targeting
// wasip1 (this repo's existing WASM-plugin toolchain), compile and run
// crypto/ecdsa + crypto/elliptic (P-256) + crypto/x509 (PKCS8 parsing) +
// crypto/sha256 + crypto/rand inside a WASM guest module, well enough to
// produce a valid ES256 (JWS) signature? This is the hard technical gate for
// an Apple WeatherKit WASM plugin, which must sign an ES256 JWT in-guest.
//
// See backend/wasm_es256_spike_test.go for the host-side round-trip test
// that proves (or disproves) the signature actually verifies.
package main

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"

	"github.com/extism/go-pdk"
)

type signInput struct {
	PrivateKeyPEM string `json:"private_key_pem"`
	SigningInput  string `json:"signing_input"` // base64url(header).base64url(claims), already computed by the caller
}

type signOutput struct {
	SignatureB64URL string `json:"signature_b64url"`
}

//go:wasmexport sign
func sign() int32 {
	var input signInput
	if err := pdk.InputJSON(&input); err != nil {
		pdk.SetError(err)
		return -1
	}

	block, _ := pem.Decode([]byte(input.PrivateKeyPEM))
	if block == nil {
		pdk.SetErrorString("failed to PEM-decode private key")
		return -1
	}

	keyAny, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		pdk.SetErrorString("ParsePKCS8PrivateKey: " + err.Error())
		return -1
	}

	priv, ok := keyAny.(*ecdsa.PrivateKey)
	if !ok {
		pdk.SetErrorString("parsed key is not an ECDSA private key")
		return -1
	}

	hash := sha256.Sum256([]byte(input.SigningInput))

	r, s, err := ecdsa.Sign(rand.Reader, priv, hash[:])
	if err != nil {
		pdk.SetErrorString("ecdsa.Sign: " + err.Error())
		return -1
	}

	// RFC 7518 ES256: signature is R||S, each a fixed-width 32-byte
	// big-endian integer (P-256 field size) -- NOT ASN1 DER.
	sigBytes := make([]byte, 64)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(sigBytes[32-len(rBytes):32], rBytes)
	copy(sigBytes[64-len(sBytes):64], sBytes)

	out := signOutput{
		SignatureB64URL: base64.RawURLEncoding.EncodeToString(sigBytes),
	}
	if err := pdk.OutputJSON(out); err != nil {
		pdk.SetError(err)
		return -1
	}
	return 0
}

func main() {}
