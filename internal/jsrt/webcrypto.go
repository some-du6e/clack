package jsrt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"hash"
	"math/big"
	"slices"
	"strings"
)

// Web Crypto's AES-GCM and ECDSA, as Chromium has them (components/webcrypto
// over BoringSSL). web.js does what Blink does: it normalizes the
// algorithms and checks each call's arguments. A CryptoKey's material is
// here, in its runtime's key store (keyStore), addressed by a handle: JS
// holds the handle and never the bytes, but what exportKey gives of a key
// it may export, as Chromium keeps a key in Blink's WebCryptoKey, out of
// script's reach. The store checks each use against the key's type,
// algorithm, usages and extractable flag, whoever calls (the host function
// is web.js's alone: it takes it off __jr.host at boot), and seals a key
// for IndexedDB with Options.KeyWrap's secret, so a stored record is no key
// to whoever reads the storage.
//
// EC keys are fixed-width big-endian fields, n bytes each (32 for P-256,
// 48 for P-384, 66 for P-521): d, then x and y of the public point.

// opError is Web Crypto's OperationError.
func opError(msg string) error { return &JSError{Name: "OperationError", Message: msg} }

// dataError is Web Crypto's DataError (key data that isn't a key).
func dataError(msg string) error { return &JSError{Name: "DataError", Message: msg} }

// gcmTags are the tag lengths Web Crypto's AES-GCM allows, in bits.
var gcmTags = []int{32, 64, 96, 104, 112, 120, 128}

// aesGCM is AES-GCM's encrypt (seal) or decrypt (open) with a tag of
// tagBits bits (one of gcmTags, checked here too). The ciphertext carries
// the tag at its end, as Web Crypto's does. Go's GCM has a 16-byte tag with
// any nonce length, so a shorter tag is the full tag's first bytes (GCM's
// MSB_t), and opening checks it by sealing the plaintext again.
func aesGCM(seal bool, key, iv, aad, data []byte, tagBits int) ([]byte, error) {
	if !slices.Contains(gcmTags, tagBits) {
		return nil, opError("The tag length is invalid: Must be 32, 64, 96, 104, 112, 120, or 128 bits")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, dataError("AES key data must be 128 or 256 bits")
	}
	if len(iv) == 0 {
		return nil, opError("The iv is empty")
	}
	g, err := cipher.NewGCMWithNonceSize(block, len(iv))
	if err != nil {
		return nil, opError(err.Error())
	}
	t := tagBits / 8
	if seal {
		out := g.Seal(nil, iv, data, aad)
		return out[:len(out)-g.Overhead()+t], nil
	}
	if len(data) < t {
		return nil, opError("")
	}
	if t == g.Overhead() {
		out, err := g.Open(nil, iv, data, aad)
		if err != nil {
			return nil, opError("")
		}
		return out, nil
	}
	ct, tag := data[:len(data)-t], data[len(data)-t:]
	// GCM's keystream is the ciphertext of zeros: the plaintext, unchecked
	ks := g.Seal(nil, iv, make([]byte, len(ct)), nil)
	pt := make([]byte, len(ct))
	subtle.XORBytes(pt, ct, ks[:len(ct)])
	again := g.Seal(nil, iv, pt, aad)
	if subtle.ConstantTimeCompare(again[len(ct):len(ct)+t], tag) != 1 {
		return nil, opError("")
	}
	return pt, nil
}

// ecCurve is a named curve's Go curves and field width.
type ecCurve struct {
	name string
	ell  elliptic.Curve
	dh   ecdh.Curve
	n    int
}

var ecCurves = []ecCurve{
	{"P-256", elliptic.P256(), ecdh.P256(), 32},
	{"P-384", elliptic.P384(), ecdh.P384(), 48},
	{"P-521", elliptic.P521(), ecdh.P521(), 66},
}

func curveNamed(name string) (ecCurve, bool) {
	for _, c := range ecCurves {
		if c.name == name {
			return c, true
		}
	}
	return ecCurve{}, false
}

// curveOf is the named curve of a Go curve.
func curveOf(c elliptic.Curve) (ecCurve, bool) {
	if c == nil {
		return ecCurve{}, false
	}
	return curveNamed(c.Params().Name)
}

// point checks that xy (x then y) is a point of c and returns its
// uncompressed encoding.
func (c ecCurve) point(xy []byte) ([]byte, error) {
	if len(xy) != 2*c.n {
		return nil, dataError("Invalid EC public key")
	}
	p := append([]byte{4}, xy...)
	if _, err := c.dh.NewPublicKey(p); err != nil {
		return nil, dataError("Invalid EC public key")
	}
	return p, nil
}

// public is the public point x||y of private scalar d.
func (c ecCurve) public(d []byte) ([]byte, error) {
	if len(d) != c.n {
		return nil, dataError("Invalid EC private key")
	}
	k, err := c.dh.NewPrivateKey(d)
	if err != nil {
		return nil, dataError("Invalid EC private key")
	}
	return k.PublicKey().Bytes()[1:], nil
}

// pair checks a private key d||x||y: d is in range and x||y is its point.
func (c ecCurve) pair(dxy []byte) (*ecdsa.PrivateKey, error) {
	if len(dxy) != 3*c.n {
		return nil, dataError("Invalid EC private key")
	}
	xy, err := c.public(dxy[:c.n])
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare(xy, dxy[c.n:]) != 1 {
		return nil, dataError("The EC private key doesn't match its public key")
	}
	return &ecdsa.PrivateKey{
		PublicKey: c.publicKey(xy),
		D:         new(big.Int).SetBytes(dxy[:c.n]),
	}, nil
}

func (c ecCurve) publicKey(xy []byte) ecdsa.PublicKey {
	return ecdsa.PublicKey{Curve: c.ell, X: new(big.Int).SetBytes(xy[:c.n]), Y: new(big.Int).SetBytes(xy[c.n:])}
}

// field is v as an n-byte big-endian field.
func field(v *big.Int, n int) []byte { return v.FillBytes(make([]byte, n)) }

func webHash(name string) (hash.Hash, bool) {
	switch name {
	case "SHA-1":
		return sha1.New(), true
	case "SHA-256":
		return sha256.New(), true
	case "SHA-384":
		return sha512.New384(), true
	case "SHA-512":
		return sha512.New(), true
	}
	return nil, false
}

// ---- the key store ----

// wcKey is one CryptoKey's material and what it may do: an AES key, or an
// EC key (x||y public, d||x||y private).
type wcKey struct {
	Type        string   `json:"t"` // "secret", "public" or "private"
	Alg         string   `json:"a"` // "AES-GCM" or "ECDSA"
	Curve       string   `json:"c,omitempty"`
	Extractable bool     `json:"e"`
	Usages      []string `json:"u"`
	Material    []byte   `json:"m"`
}

// keyStore is a runtime's CryptoKeys by handle (JS thread only).
type keyStore struct {
	keys map[uint32]*wcKey
	next uint32
	seal cipher.AEAD // sealing keys for storage; made on first use
}

func (r *Runtime) keyStore() *keyStore {
	if r.wcKeys == nil {
		r.wcKeys = &keyStore{keys: map[uint32]*wcKey{}}
	}
	return r.wcKeys
}

func (s *keyStore) add(k *wcKey) float64 {
	s.next++
	s.keys[s.next] = k
	return float64(s.next)
}

// sealedKeyAAD binds a sealed key to what it is.
const sealedKeyAAD = "jsrt CryptoKey v1"

// sealer is the AEAD that seals keys for storage: Options.KeyWrap's
// 32-byte secret, the same at every start, or this runtime's own (a
// stored key then opens only in it).
func (r *Runtime) sealer() (cipher.AEAD, error) {
	s := r.keyStore()
	if s.seal != nil {
		return s.seal, nil
	}
	var secret []byte
	if r.opts.KeyWrap != nil {
		b, err := r.opts.KeyWrap()
		if err != nil {
			return nil, opError("jsrt: KeyWrap: " + err.Error())
		}
		if len(b) != 32 {
			return nil, opError(fmt.Sprintf("jsrt: KeyWrap's secret is %d bytes, want 32", len(b)))
		}
		secret = b
	} else {
		secret = make([]byte, 32)
		_, _ = rand.Read(secret)
	}
	block, err := aes.NewCipher(secret)
	if err != nil {
		return nil, opError(err.Error())
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return nil, opError(err.Error())
	}
	s.seal = g
	return g, nil
}

// keyErr is Web Crypto's InvalidAccessError for a key that can't do it.
func keyErr(msg string) error { return &JSError{Name: "InvalidAccessError", Message: msg} }

func syntaxErr(msg string) error { return &JSError{Name: "SyntaxError", Message: msg} }

// usagesOf reads web.js's comma-separated usages.
func usagesOf(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, ",")
}

// onlyUsages: each of us is one of allowed (Web Crypto's SyntaxError).
func onlyUsages(us []string, allowed ...string) error {
	for _, u := range us {
		if !slices.Contains(allowed, u) {
			return syntaxErr("Cannot create a key using the specified key usages.")
		}
	}
	return nil
}

// ecKeyOf makes an EC key of typ from fields (x||y, or d||x||y), checked.
func ecKeyOf(c ecCurve, typ string, fields []byte, extractable bool, usages []string) (*wcKey, error) {
	if typ == "public" {
		if err := onlyUsages(usages, "verify"); err != nil {
			return nil, err
		}
		if _, err := c.point(fields); err != nil {
			return nil, err
		}
	} else {
		if err := onlyUsages(usages, "sign"); err != nil {
			return nil, err
		}
		if len(usages) == 0 {
			return nil, syntaxErr("Usages cannot be empty when creating a key.")
		}
		if _, err := c.pair(fields); err != nil {
			return nil, err
		}
	}
	return &wcKey{Type: typ, Alg: "ECDSA", Curve: c.name, Extractable: extractable, Usages: usages, Material: slices.Clone(fields)}, nil
}

// webCrypto is web.js's host function webcrypto(op, ...): one Web Crypto
// operation over the runtime's key store.
func (r *Runtime) webCrypto(op string, a []Value) (any, error) {
	ks := r.keyStore()
	arg := func(i int) []byte {
		if i < len(a) {
			return a[i].Bytes()
		}
		return nil
	}
	str := func(i int) string {
		if i < len(a) {
			return a[i].String()
		}
		return ""
	}
	flag := func(i int) bool { return i < len(a) && a[i].Bool() }
	num := func(i int) int {
		if i < len(a) {
			return a[i].Int()
		}
		return 0
	}
	curve := func(i int) (ecCurve, error) {
		c, ok := curveNamed(str(i))
		if !ok {
			return c, &JSError{Name: "NotSupportedError", Message: "Unrecognized namedCurve"}
		}
		return c, nil
	}
	key := func(i int) (*wcKey, error) {
		if i < len(a) {
			if k := ks.keys[uint32(a[i].Int())]; k != nil {
				return k, nil
			}
		}
		return nil, keyErr("The key is no longer valid")
	}
	switch op {
	case "aes-generate": // (bits, extractable, usages) → handle
		bits, us := num(0), usagesOf(str(2))
		if err := onlyUsages(us, "encrypt", "decrypt", "wrapKey", "unwrapKey"); err != nil {
			return nil, err
		}
		if bits != 128 && bits != 256 {
			return nil, opError("AES key length must be 128 or 256 bits")
		}
		m := make([]byte, bits/8)
		_, _ = rand.Read(m)
		return ks.add(&wcKey{Type: "secret", Alg: "AES-GCM", Extractable: flag(1), Usages: us, Material: m}), nil

	case "aes-import": // (raw, extractable, usages) → handle
		raw, us := arg(0), usagesOf(str(2))
		if err := onlyUsages(us, "encrypt", "decrypt", "wrapKey", "unwrapKey"); err != nil {
			return nil, err
		}
		switch len(raw) {
		case 16, 32:
		case 24:
			return nil, opError("192-bit AES keys are not supported")
		default:
			return nil, dataError("AES key data must be 128 or 256 bits")
		}
		return ks.add(&wcKey{Type: "secret", Alg: "AES-GCM", Extractable: flag(1), Usages: us, Material: slices.Clone(raw)}), nil

	case "ec-generate": // (curve, extractable, sign usages, verify usages) → [private, public]
		c, err := curve(0)
		if err != nil {
			return nil, err
		}
		k, err := c.dh.GenerateKey(rand.Reader)
		if err != nil {
			return nil, opError(err.Error())
		}
		xy := k.PublicKey().Bytes()[1:]
		priv, err := ecKeyOf(c, "private", append(k.Bytes(), xy...), flag(1), usagesOf(str(2)))
		if err != nil {
			return nil, err
		}
		pub, err := ecKeyOf(c, "public", xy, true, usagesOf(str(3)))
		if err != nil {
			return nil, err
		}
		return []any{ks.add(priv), ks.add(pub)}, nil

	case "ec-import": // (format, curve, data, extractable, usages) → [handle, type]
		c, err := curve(1)
		if err != nil {
			return nil, err
		}
		data, ext, us := arg(2), flag(3), usagesOf(str(4))
		var typ string
		var fields []byte
		switch str(0) {
		case "raw": // 04||x||y or 02/03||x
			typ = "public"
			switch {
			case len(data) == 1+2*c.n && data[0] == 4:
				fields = data[1:]
			case len(data) == 1+c.n && (data[0] == 2 || data[0] == 3):
				x, y := elliptic.UnmarshalCompressed(c.ell, data)
				if x == nil {
					return nil, dataError("Invalid EC public key")
				}
				fields = append(field(x, c.n), field(y, c.n)...)
			default:
				return nil, dataError("Invalid keyData")
			}
		case "xy": // a JWK's x and y
			typ, fields = "public", data
		case "dxy": // a JWK's d, x and y
			typ, fields = "private", data
		case "spki":
			k, err := x509.ParsePKIXPublicKey(data)
			if err != nil {
				return nil, dataError("Invalid keyData")
			}
			pub, ok := k.(*ecdsa.PublicKey)
			if !ok {
				return nil, dataError("The key is not an EC key")
			}
			kc, ok := curveOf(pub.Curve)
			if !ok || kc.name != c.name {
				return nil, dataError("The key's namedCurve doesn't match the one requested")
			}
			typ, fields = "public", append(field(pub.X, c.n), field(pub.Y, c.n)...)
		case "pkcs8":
			k, err := x509.ParsePKCS8PrivateKey(data)
			if err != nil {
				return nil, dataError("Invalid keyData")
			}
			priv, ok := k.(*ecdsa.PrivateKey)
			if !ok {
				return nil, dataError("The key is not an EC key")
			}
			kc, ok := curveOf(priv.Curve)
			if !ok || kc.name != c.name {
				return nil, dataError("The key's namedCurve doesn't match the one requested")
			}
			d := field(priv.D, c.n)
			xy, err := c.public(d) // the public point d gives, whatever the file says
			if err != nil {
				return nil, err
			}
			typ, fields = "private", append(d, xy...)
		default:
			return nil, &JSError{Name: "NotSupportedError", Message: "Unsupported import key format for algorithm"}
		}
		k, err := ecKeyOf(c, typ, fields, ext, us)
		if err != nil {
			return nil, err
		}
		return []any{ks.add(k), typ}, nil

	case "export": // (handle, format) → bytes; "jwk" gives the fields (AES k; EC x||y or d||x||y)
		k, err := key(0)
		if err != nil {
			return nil, err
		}
		if !k.Extractable {
			return nil, keyErr("key is not extractable")
		}
		f := str(1)
		if k.Alg == "AES-GCM" {
			if f != "raw" && f != "jwk" {
				return nil, &JSError{Name: "NotSupportedError", Message: "Unsupported export key format for algorithm"}
			}
			return slices.Clone(k.Material), nil
		}
		c, _ := curveNamed(k.Curve)
		switch {
		case f == "jwk":
			return slices.Clone(k.Material), nil
		case (f == "raw" || f == "spki") && k.Type != "public", f == "pkcs8" && k.Type != "private":
			return nil, keyErr("The key is not of the expected type")
		case f == "raw":
			return append([]byte{4}, k.Material...), nil
		case f == "spki":
			pub := c.publicKey(k.Material)
			der, err := x509.MarshalPKIXPublicKey(&pub)
			if err != nil {
				return nil, opError(err.Error())
			}
			return der, nil
		case f == "pkcs8":
			p, err := c.pair(k.Material)
			if err != nil {
				return nil, opError(err.Error())
			}
			der, err := x509.MarshalPKCS8PrivateKey(p)
			if err != nil {
				return nil, opError(err.Error())
			}
			return der, nil
		}
		return nil, &JSError{Name: "NotSupportedError", Message: "Unsupported export key format for algorithm"}

	case "aes-gcm": // (handle, usage, iv, additionalData, tagBits, data)
		k, err := key(0)
		if err != nil {
			return nil, err
		}
		use := str(1)
		if k.Alg != "AES-GCM" {
			return nil, keyErr("The requested operation is not valid for the provided key")
		}
		if !slices.Contains(k.Usages, use) {
			return nil, keyErr("key.usages does not permit this operation")
		}
		switch use {
		case "encrypt", "wrapKey":
			return aesGCM(true, k.Material, arg(2), arg(3), arg(5), num(4))
		case "decrypt", "unwrapKey":
			return aesGCM(false, k.Material, arg(2), arg(3), arg(5), num(4))
		}
		return nil, keyErr("key.usages does not permit this operation")

	case "ec-sign": // (handle, hash, data) → r||s
		k, err := key(0)
		if err != nil {
			return nil, err
		}
		if k.Alg != "ECDSA" || k.Type != "private" || !slices.Contains(k.Usages, "sign") {
			return nil, keyErr("The requested operation is not valid for the provided key")
		}
		c, _ := curveNamed(k.Curve)
		h, ok := webHash(str(1))
		if !ok {
			return nil, &JSError{Name: "NotSupportedError", Message: "Unrecognized hash"}
		}
		p, err := c.pair(k.Material)
		if err != nil {
			return nil, opError(err.Error())
		}
		h.Write(arg(2))
		rr, ss, err := ecdsa.Sign(rand.Reader, p, h.Sum(nil))
		if err != nil {
			return nil, opError(err.Error())
		}
		return append(field(rr, c.n), field(ss, c.n)...), nil

	case "ec-verify": // (handle, hash, signature, data) → bool
		k, err := key(0)
		if err != nil {
			return nil, err
		}
		if k.Alg != "ECDSA" || k.Type != "public" || !slices.Contains(k.Usages, "verify") {
			return nil, keyErr("The requested operation is not valid for the provided key")
		}
		c, _ := curveNamed(k.Curve)
		h, ok := webHash(str(1))
		if !ok {
			return nil, &JSError{Name: "NotSupportedError", Message: "Unrecognized hash"}
		}
		sig := arg(2)
		if len(sig) != 2*c.n { // Web Crypto: a signature of the wrong length doesn't verify
			return false, nil
		}
		h.Write(arg(3))
		pub := c.publicKey(k.Material)
		rr, ss := new(big.Int).SetBytes(sig[:c.n]), new(big.Int).SetBytes(sig[c.n:])
		return ecdsa.Verify(&pub, h.Sum(nil), rr, ss), nil

	case "key-clone": // (handle) → a new handle to the same key (structuredClone)
		k, err := key(0)
		if err != nil {
			return nil, err
		}
		c := *k
		c.Usages, c.Material = slices.Clone(k.Usages), slices.Clone(k.Material)
		return ks.add(&c), nil

	case "key-free": // (handle): its CryptoKey was collected
		if len(a) > 0 {
			delete(ks.keys, uint32(a[0].Int()))
		}
		return nil, nil

	case "key-seal": // (handle) → the key sealed for storage
		k, err := key(0)
		if err != nil {
			return nil, err
		}
		g, err := r.sealer()
		if err != nil {
			return nil, err
		}
		plain, err := json.Marshal(k)
		if err != nil {
			return nil, opError(err.Error())
		}
		nonce := make([]byte, g.NonceSize())
		_, _ = rand.Read(nonce)
		return append(append([]byte{1}, nonce...), g.Seal(nil, nonce, plain, []byte(sealedKeyAAD))...), nil

	case "key-open": // (sealed) → {h, t, a, c, l, e, u}: a new handle to the stored key
		b := arg(0)
		g, err := r.sealer()
		if err != nil {
			return nil, err
		}
		if len(b) < 1+g.NonceSize() || b[0] != 1 {
			return nil, dataError("The stored key can't be read")
		}
		plain, err := g.Open(nil, b[1:1+g.NonceSize()], b[1+g.NonceSize():], []byte(sealedKeyAAD))
		if err != nil {
			return nil, dataError("The stored key can't be read here")
		}
		var k wcKey
		if err := json.Unmarshal(plain, &k); err != nil {
			return nil, dataError("The stored key can't be read")
		}
		if k.Usages == nil {
			k.Usages = []string{}
		}
		return map[string]any{"h": ks.add(&k), "t": k.Type, "a": k.Alg, "c": k.Curve, "l": len(k.Material) * 8, "e": k.Extractable, "u": k.Usages}, nil
	}
	return nil, &JSError{Name: "NotSupportedError", Message: "webcrypto: unknown operation " + op}
}
