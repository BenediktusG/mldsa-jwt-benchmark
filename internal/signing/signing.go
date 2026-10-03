package signing

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/mldsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"

	"mldsa-jwt-benchmark/internal/profile"
)

type Signer interface {
	Sign([]byte) ([]byte, error)
	Verify([]byte, []byte) error
}

type diskKey struct {
	Algorithm string `json:"algorithm"`
	Private   string `json:"private"`
	Public    string `json:"public"`
}

func params(alg string) (mldsa.Parameters, error) {
	switch alg {
	case "ML-DSA-44":
		return mldsa.MLDSA44(), nil
	case "ML-DSA-65":
		return mldsa.MLDSA65(), nil
	case "ML-DSA-87":
		return mldsa.MLDSA87(), nil
	default:
		return mldsa.Parameters{}, errors.New("unsupported ML-DSA algorithm")
	}
}

func curve(alg string) (elliptic.Curve, error) {
	switch alg {
	case "ES256":
		return elliptic.P256(), nil
	case "ES384":
		return elliptic.P384(), nil
	case "ES512":
		return elliptic.P521(), nil
	default:
		return nil, errors.New("unsupported ECDSA algorithm")
	}
}

func GenerateFiles(c profile.Config) error {
	if err := os.MkdirAll(c.KeyDir, 0700); err != nil {
		return err
	}
	for _, alg := range profile.Algorithms {
		if _, err := os.Stat(c.KeyPath(alg)); err == nil {
			return fmt.Errorf("key already exists: %s", alg)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	for _, alg := range profile.Algorithms {
		var priv, pub []byte
		var err error
		if alg[0] == 'E' {
			cv, _ := curve(alg)
			k, e := ecdsa.GenerateKey(cv, rand.Reader)
			if e != nil {
				return e
			}
			priv, err = x509.MarshalPKCS8PrivateKey(k)
			if err != nil {
				return err
			}
			pub, err = x509.MarshalPKIXPublicKey(&k.PublicKey)
		} else {
			p, _ := params(alg)
			k, e := mldsa.GenerateKey(p)
			if e != nil {
				return e
			}
			priv, pub = k.Bytes(), k.PublicKey().Bytes()
		}
		if err != nil {
			return err
		}
		b, err := json.Marshal(diskKey{alg, base64.StdEncoding.EncodeToString(priv), base64.StdEncoding.EncodeToString(pub)})
		if err != nil {
			return err
		}
		f, err := os.OpenFile(c.KeyPath(alg), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return fmt.Errorf("create %s: %w", alg, err)
		}
		if _, err = f.Write(b); err != nil {
			f.Close()
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
	}
	return nil
}

func LoadAll(c profile.Config) (map[string]Signer, error) {
	out := make(map[string]Signer, len(profile.Algorithms))
	for _, alg := range profile.Algorithms {
		fi, err := os.Stat(c.KeyPath(alg))
		if err != nil {
			return nil, err
		}
		if fi.Mode().Perm()&0077 != 0 {
			return nil, fmt.Errorf("key %s has excessive permissions", alg)
		}
		b, err := os.ReadFile(c.KeyPath(alg))
		if err != nil {
			return nil, err
		}
		var d diskKey
		if err := json.Unmarshal(b, &d); err != nil {
			return nil, err
		}
		if d.Algorithm != alg {
			return nil, fmt.Errorf("key algorithm mismatch: %s", alg)
		}
		priv, err := base64.StdEncoding.DecodeString(d.Private)
		if err != nil {
			return nil, err
		}
		pub, err := base64.StdEncoding.DecodeString(d.Public)
		if err != nil {
			return nil, err
		}
		if alg[0] == 'E' {
			kAny, err := x509.ParsePKCS8PrivateKey(priv)
			if err != nil {
				return nil, err
			}
			k, ok := kAny.(*ecdsa.PrivateKey)
			if !ok {
				return nil, fmt.Errorf("not ECDSA key: %s", alg)
			}
			cv, _ := curve(alg)
			if k.Curve.Params().Name != cv.Params().Name {
				return nil, fmt.Errorf("wrong curve: %s", alg)
			}
			got, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
			if err != nil || !bytes.Equal(got, pub) {
				return nil, fmt.Errorf("public key mismatch: %s", alg)
			}
			out[alg] = &ecdsaSigner{alg: alg, key: k}
		} else {
			p, _ := params(alg)
			k, err := mldsa.NewPrivateKey(p, priv)
			if err != nil {
				return nil, err
			}
			pk, err := mldsa.NewPublicKey(p, pub)
			if err != nil || !bytes.Equal(k.PublicKey().Bytes(), pk.Bytes()) {
				return nil, fmt.Errorf("public key mismatch: %s", alg)
			}
			out[alg] = &mldsaSigner{key: k, public: pk}
		}
	}
	return out, nil
}

type ecdsaSigner struct {
	alg string
	key *ecdsa.PrivateKey
}

func (s *ecdsaSigner) digest(message []byte) []byte {
	switch s.alg {
	case "ES256":
		h := sha256.Sum256(message)
		return h[:]
	case "ES384":
		h := sha512.Sum384(message)
		return h[:]
	default:
		h := sha512.Sum512(message)
		return h[:]
	}
}

func (s *ecdsaSigner) Sign(message []byte) ([]byte, error) {
	r, sigS, err := ecdsa.Sign(rand.Reader, s.key, s.digest(message))
	if err != nil {
		return nil, err
	}
	n := (s.key.Curve.Params().BitSize + 7) / 8
	b := make([]byte, 2*n)
	r.FillBytes(b[:n])
	sigS.FillBytes(b[n:])
	return b, nil
}

func (s *ecdsaSigner) Verify(message, sig []byte) error {
	n := (s.key.Curve.Params().BitSize + 7) / 8
	if len(sig) != 2*n || !ecdsa.Verify(&s.key.PublicKey, s.digest(message), new(big.Int).SetBytes(sig[:n]), new(big.Int).SetBytes(sig[n:])) {
		return errors.New("invalid signature")
	}
	return nil
}

type mldsaSigner struct {
	key    *mldsa.PrivateKey
	public *mldsa.PublicKey
}

func (s *mldsaSigner) Sign(message []byte) ([]byte, error) {
	return s.key.Sign(nil, message, &mldsa.Options{Context: ""})
}

func (s *mldsaSigner) Verify(message, sig []byte) error {
	return mldsa.Verify(s.public, message, sig, &mldsa.Options{Context: ""})
}
