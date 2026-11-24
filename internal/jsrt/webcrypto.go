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
	"hash"
	"math/big"
)

// Web Crypto's AES-GCM and ECDSA, as Chromium has them (components/webcrypto
// over BoringSSL). web.js does what Blink does: it normalizes the
// algorithms, checks each key's type and usages, and keeps every
// CryptoKey's material in its internal slots. The arithmetic is here, over
// bytes: nothing is kept between calls.
//
// EC keys travel as fixed-width big-endian fields, n bytes each (32 for
// P-256, 48 for P-384, 66 for P-521): d, then x and y of the public point.

// opError is Web Crypto's OperationError.
func opError(msg string) error { return &JSError{Name: "OperationError", Message: msg} }

// dataError is Web Crypto's DataError (key data that isn't a key).
func dataError(msg string) error { return &JSError{Name: "DataError", Message: msg} }

// aesGCM is AES-GCM's encrypt (seal) or decrypt (open) with a tag of
// tagBits bits (Web Crypto allows 32, 64, 96, 104, 112, 120 and 128; web.js
// checks). The ciphertext carries the tag at its end, as Web Crypto's
// does. Go's GCM has a 16-byte tag with any nonce length, so a shorter tag
// is the full tag's first bytes (GCM's MSB_t), and opening checks it by
// sealing the plaintext again.
func aesGCM(seal bool, key, iv, aad, data []byte, tagBits int) ([]byte, error) {
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

// curveCode is a named curve's code in parsed keys (web.js's EC_CODES).
func curveCode(name string) byte {
	for i, c := range ecCurves {
		if c.name == name {
			return byte(i + 1)
		}
	}
	return 0
}

// webCrypto is web.js's H.webcrypto(op, ...): one Web Crypto primitive.
func webCrypto(op string, a []Value) (any, error) {
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
	curve := func(i int) (ecCurve, error) {
		c, ok := curveNamed(str(i))
		if !ok {
			return c, &JSError{Name: "NotSupportedError", Message: "Unrecognized namedCurve"}
		}
		return c, nil
	}
	switch op {
	case "aes-gcm-seal", "aes-gcm-open":
		bits := 128
		if len(a) > 3 {
			bits = a[3].Int()
		}
		return aesGCM(op == "aes-gcm-seal", arg(0), arg(1), arg(2), arg(4), bits)

	case "ec-generate": // (curve) → d||x||y
		c, err := curve(0)
		if err != nil {
			return nil, err
		}
		k, err := c.dh.GenerateKey(rand.Reader)
		if err != nil {
			return nil, opError(err.Error())
		}
		return append(k.Bytes(), k.PublicKey().Bytes()[1:]...), nil

	case "ec-public": // (curve, d) → x||y
		c, err := curve(0)
		if err != nil {
			return nil, err
		}
		return c.public(arg(1))

	case "ec-check-pair": // (curve, d||x||y)
		c, err := curve(0)
		if err != nil {
			return nil, err
		}
		_, err = c.pair(arg(1))
		return err == nil, err

	case "ec-check-point": // (curve, x||y)
		c, err := curve(0)
		if err != nil {
			return nil, err
		}
		_, err = c.point(arg(1))
		return err == nil, err

	case "ec-decompress": // (curve, 02/03||x) → x||y
		c, err := curve(0)
		if err != nil {
			return nil, err
		}
		x, y := elliptic.UnmarshalCompressed(c.ell, arg(1))
		if x == nil {
			return nil, dataError("Invalid EC public key")
		}
		return append(field(x, c.n), field(y, c.n)...), nil

	case "ec-sign": // (curve, hash, d||x||y, data) → r||s
		c, err := curve(0)
		if err != nil {
			return nil, err
		}
		h, ok := webHash(str(1))
		if !ok {
			return nil, &JSError{Name: "NotSupportedError", Message: "Unrecognized hash"}
		}
		k, err := c.pair(arg(2))
		if err != nil {
			return nil, opError(err.Error())
		}
		h.Write(arg(3))
		r, s, err := ecdsa.Sign(rand.Reader, k, h.Sum(nil))
		if err != nil {
			return nil, opError(err.Error())
		}
		return append(field(r, c.n), field(s, c.n)...), nil

	case "ec-verify": // (curve, hash, x||y, signature, data) → bool
		c, err := curve(0)
		if err != nil {
			return nil, err
		}
		h, ok := webHash(str(1))
		if !ok {
			return nil, &JSError{Name: "NotSupportedError", Message: "Unrecognized hash"}
		}
		xy, sig := arg(2), arg(3)
		if _, err := c.point(xy); err != nil {
			return nil, opError(err.Error())
		}
		if len(sig) != 2*c.n { // Web Crypto: a signature of the wrong length doesn't verify
			return false, nil
		}
		h.Write(arg(4))
		pub := c.publicKey(xy)
		r, s := new(big.Int).SetBytes(sig[:c.n]), new(big.Int).SetBytes(sig[c.n:])
		return ecdsa.Verify(&pub, h.Sum(nil), r, s), nil

	case "ec-spki": // (curve, x||y) → SubjectPublicKeyInfo DER
		c, err := curve(0)
		if err != nil {
			return nil, err
		}
		if _, err := c.point(arg(1)); err != nil {
			return nil, err
		}
		pub := c.publicKey(arg(1))
		der, err := x509.MarshalPKIXPublicKey(&pub)
		if err != nil {
			return nil, opError(err.Error())
		}
		return der, nil

	case "ec-pkcs8": // (curve, d||x||y) → PrivateKeyInfo DER
		c, err := curve(0)
		if err != nil {
			return nil, err
		}
		k, err := c.pair(arg(1))
		if err != nil {
			return nil, err
		}
		der, err := x509.MarshalPKCS8PrivateKey(k)
		if err != nil {
			return nil, opError(err.Error())
		}
		return der, nil

	case "ec-parse-spki": // (DER) → curve code, then x||y
		k, err := x509.ParsePKIXPublicKey(arg(0))
		if err != nil {
			return nil, dataError("Invalid keyData")
		}
		pub, ok := k.(*ecdsa.PublicKey)
		if !ok {
			return nil, dataError("The key is not an EC key")
		}
		c, ok := curveOf(pub.Curve)
		if !ok {
			return nil, &JSError{Name: "NotSupportedError", Message: "Unrecognized namedCurve"}
		}
		return append([]byte{curveCode(c.name)}, append(field(pub.X, c.n), field(pub.Y, c.n)...)...), nil

	case "ec-parse-pkcs8": // (DER) → curve code, then d||x||y
		k, err := x509.ParsePKCS8PrivateKey(arg(0))
		if err != nil {
			return nil, dataError("Invalid keyData")
		}
		priv, ok := k.(*ecdsa.PrivateKey)
		if !ok {
			return nil, dataError("The key is not an EC key")
		}
		c, ok := curveOf(priv.Curve)
		if !ok {
			return nil, &JSError{Name: "NotSupportedError", Message: "Unrecognized namedCurve"}
		}
		d := field(priv.D, c.n)
		xy, err := c.public(d) // the public point d gives, whatever the file says
		if err != nil {
			return nil, err
		}
		return append([]byte{curveCode(c.name)}, append(d, xy...)...), nil
	}
	return nil, &JSError{Name: "NotSupportedError", Message: "webcrypto: unknown operation " + op}
}
