// Disposable feasibility spike (see backend/testdata/wasm_plugins/src/es256sign/main.go
// for the "why"). Not wired into any real plugin contract; proves whether
// TinyGo 0.41.1 -> wasip1 can sign an ES256 JWT in-guest, for a future Apple
// WeatherKit WASM plugin. Safe to delete this file plus
// backend/testdata/wasm_plugins/ once that question is settled elsewhere.
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"testing"

	extism "github.com/extism/go-sdk"
)

const es256signFixtureWasm = "testdata/wasm_plugins/es256sign.wasm"

type es256SignInput struct {
	PrivateKeyPEM string `json:"private_key_pem"`
	SigningInput  string `json:"signing_input"`
}

type es256SignOutput struct {
	SignatureB64URL string `json:"signature_b64url"`
}

func TestES256SpikeGuestSigningVerifiesAgainstHostPublicKey(t *testing.T) {
	// 1. Generate a throwaway P-256 key host-side and PKCS8-PEM-encode it,
	// exactly like a real ECDSA private key delivered via settings.yaml
	// would be.
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey failed: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	// 2. Build a sample JWS signing_input (base64url(header).base64url(claims)),
	// same shape a WeatherKit-plugin caller would construct.
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES256","kid":"TESTKID1234"}`))
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"TESTTEAMID","iat":1752900000,"exp":1752986400,"sub":"com.example.weatherkit"}`))
	signingInput := header + "." + claims

	// 3. Load the compiled es256sign.wasm spike via the same extism/go-sdk
	// pattern as wasm_tide_provider.go (CompiledPlugin, EnableWasi:true, a
	// fresh Instance per call).
	ctx := context.Background()
	manifest := extism.Manifest{
		Wasm: []extism.Wasm{extism.WasmFile{Path: es256signFixtureWasm}},
	}
	compiled, err := extism.NewCompiledPlugin(ctx, manifest, extism.PluginConfig{EnableWasi: true}, []extism.HostFunction{})
	if err != nil {
		t.Fatalf("NewCompiledPlugin failed: %v", err)
	}
	instance, err := compiled.Instance(ctx, extism.PluginInstanceConfig{ModuleConfig: wasmModuleConfig()})
	if err != nil {
		t.Fatalf("compiled.Instance failed: %v", err)
	}
	defer instance.Close(ctx)

	input, err := json.Marshal(es256SignInput{
		PrivateKeyPEM: string(pemBytes),
		SigningInput:  signingInput,
	})
	if err != nil {
		t.Fatalf("marshal input failed: %v", err)
	}

	_, out, err := instance.Call("sign", input)
	if err != nil {
		t.Fatalf("guest sign() call failed: %v", err)
	}

	var result es256SignOutput
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal guest output failed: %v (raw: %s)", err, out)
	}

	sigBytes, err := base64.RawURLEncoding.DecodeString(result.SignatureB64URL)
	if err != nil {
		t.Fatalf("decode signature_b64url failed: %v", err)
	}
	if len(sigBytes) != 64 {
		t.Fatalf("expected a 64-byte R||S ES256 signature, got %d bytes", len(sigBytes))
	}

	// 4. Verify the round trip actually cryptographically verifies against
	// the host-held public key -- producing SOME bytes back is not success
	// on its own.
	r := new(big.Int).SetBytes(sigBytes[:32])
	s := new(big.Int).SetBytes(sigBytes[32:])
	hash := sha256.Sum256([]byte(signingInput))

	if !ecdsa.Verify(&priv.PublicKey, hash[:], r, s) {
		t.Fatalf("guest-produced ES256 signature does NOT verify against the host public key")
	}
}
