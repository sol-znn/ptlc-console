// This file is not part of the sim module. scripts/fetch-go-deps.sh adds it
// to a copy of github.com/ethereum/go-ethereum/crypto/secp256k1 (README.md
// beside this file).
//
// The package is a cgo binding to libsecp256k1, and a browser build has no
// cgo: without this file it compiles to a curve with no way to sign or
// recover. These are the four functions go-zenon calls, with the signatures
// and the errors of secp256.go, on the pure Go implementation that
// go-ethereum's own crypto package already uses when cgo is off.

//go:build !cgo

package secp256k1

import (
	"errors"
	"math/big"

	dcr "github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

var (
	ErrInvalidMsgLen       = errors.New("invalid message length, need 32 bytes")
	ErrInvalidSignatureLen = errors.New("invalid signature length")
	ErrInvalidRecoveryID   = errors.New("invalid signature recovery id")
	ErrInvalidKey          = errors.New("invalid private key")
	ErrInvalidPubkey       = errors.New("invalid public key")
	ErrSignFailed          = errors.New("signing failed")
	ErrRecoverFailed       = errors.New("recovery failed")
)

// Sign creates a recoverable ECDSA signature, [R || S || V] with V 0 or 1.
func Sign(msg []byte, seckey []byte) ([]byte, error) {
	if len(msg) != 32 {
		return nil, ErrInvalidMsgLen
	}
	if len(seckey) != 32 {
		return nil, ErrInvalidKey
	}
	var scalar dcr.ModNScalar
	if overflow := scalar.SetByteSlice(seckey); overflow || scalar.IsZero() {
		return nil, ErrInvalidKey
	}
	key := dcr.NewPrivateKey(&scalar)
	defer key.Zero()
	// SignCompact answers [27+V || R || S].
	compact := ecdsa.SignCompact(key, msg, false)
	sig := make([]byte, 65)
	copy(sig, compact[1:])
	sig[64] = compact[0] - 27
	return sig, nil
}

// RecoverPubkey returns the public key of the signer, 65 bytes uncompressed.
func RecoverPubkey(msg []byte, sig []byte) ([]byte, error) {
	if len(msg) != 32 {
		return nil, ErrInvalidMsgLen
	}
	if err := checkSignature(sig); err != nil {
		return nil, err
	}
	compact := make([]byte, 65)
	compact[0] = sig[64] + 27
	copy(compact[1:], sig[:64])
	pub, _, err := ecdsa.RecoverCompact(compact, msg)
	if err != nil {
		return nil, ErrRecoverFailed
	}
	return pub.SerializeUncompressed(), nil
}

// VerifySignature checks a 64 byte [R || S] signature. As in secp256.go, a
// signature whose S is in the upper half of the order is refused.
func VerifySignature(pubkey, msg, signature []byte) bool {
	if len(msg) != 32 || len(signature) != 64 || len(pubkey) == 0 {
		return false
	}
	var r, s dcr.ModNScalar
	if r.SetByteSlice(signature[:32]) || s.SetByteSlice(signature[32:]) {
		return false
	}
	if s.IsOverHalfOrder() {
		return false
	}
	key, err := dcr.ParsePubKey(pubkey)
	if err != nil {
		return false
	}
	return ecdsa.NewSignature(&r, &s).Verify(msg, key)
}

// DecompressPubkey parses a 33 byte compressed key into its coordinates.
func DecompressPubkey(pubkey []byte) (x, y *big.Int) {
	if len(pubkey) != 33 {
		return nil, nil
	}
	key, err := dcr.ParsePubKey(pubkey)
	if err != nil {
		return nil, nil
	}
	return key.X(), key.Y()
}

// CompressPubkey encodes a public key to its 33 byte compressed form.
func CompressPubkey(x, y *big.Int) []byte {
	var fx, fy dcr.FieldVal
	fx.SetByteSlice(x.Bytes())
	fy.SetByteSlice(y.Bytes())
	return dcr.NewPublicKey(&fx, &fy).SerializeCompressed()
}

func checkSignature(sig []byte) error {
	if len(sig) != 65 {
		return ErrInvalidSignatureLen
	}
	if sig[64] >= 4 {
		return ErrInvalidRecoveryID
	}
	return nil
}
