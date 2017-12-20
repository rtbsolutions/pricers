package doubleclick

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"

	"github.com/benjaminch/openrtb-pricers/helpers"

	"github.com/golang/glog"
)

// DoubleClickPricer implementing price encryption and decryption
// Specs : https://developers.google.com/ad-exchange/rtb/response-guide/decrypt-price
type DoubleClickPricer struct {
	encryptionKey   string
	integrityKey    string
	keyDecodingMode helpers.KeyDecodingMode
	scaleFactor     float64
	isDebugMode     bool
}

func NewDoubleClickPricer(encryptionKey string,
	integrityKey string,
	keyDecodingMode helpers.KeyDecodingMode,
	scaleFactor float64,
	isDebugMode bool) (*DoubleClickPricer, error) {
	var err error
	return &DoubleClickPricer{
			encryptionKey:   encryptionKey,
			integrityKey:    integrityKey,
			keyDecodingMode: keyDecodingMode,
			scaleFactor:     scaleFactor,
			isDebugMode:     isDebugMode},
		err
}

// Encrypt is returned by FormFile when the provided file field name
// is either not present in the request or not a file field.
func (dc *DoubleClickPricer) Encrypt(
	seed string,
	price float64,
	isDebugMode bool) (string, error) {
	var err error
	var encryptedPrice string

	encryptingFun, err := helpers.CreateHmac(dc.encryptionKey, dc.keyDecodingMode)
	if err != nil {
		return encryptedPrice, err
	}
	
	integrityFun, err := helpers.CreateHmac(dc.integrityKey, dc.keyDecodingMode)
	if err != nil {
		return encryptedPrice, nil
	}

	// Result
	var (
		iv        [16]byte
		encoded   [8]byte
		signature [4]byte
	)

	if isDebugMode == true {
		glog.Info("Keys decoding mode : ", dc.keyDecodingMode)
		glog.Info("Encryption key : ", dc.encryptionKey)
		encryptionKeyHexa, err := hex.DecodeString(dc.encryptionKey)
		if err != nil {
			return encryptedPrice, err
		}
		glog.Info("Encryption key (bytes) : ", []byte(encryptionKeyHexa))
		glog.Info("Integrity key : ", dc.integrityKey)
		integrityKeyHexa, err := hex.DecodeString(dc.integrityKey)
		if err != nil {
			return encryptedPrice, err
		}
		glog.Info("Integrity key (bytes) : ", []byte(integrityKeyHexa))
	}

	data := helpers.ApplyScaleFactor(price, dc.scaleFactor, isDebugMode)

	// Create Initialization Vector from seed
	sum := md5.Sum([]byte(seed))
	copy(iv[:], sum[:])
	if isDebugMode == true {
		glog.Info("Seed : ", seed)
		glog.Info("Initialization vector : ", iv)
	}

	//pad = hmac(e_key, iv), first 8 bytes
	pad := helpers.HmacSum(encryptingFun, iv[:])[:8]
	if isDebugMode == true {
		glog.Info("// pad = hmac(e_key, iv), first 8 bytes")
		glog.Info("Pad : ", pad)
	}

	// enc_data = pad <xor> data
	for i := range data {
		encoded[i] = pad[i] ^ data[i]
	}
	if isDebugMode == true {
		glog.Info("// enc_data = pad <xor> data")
		glog.Info("Encoded price bytes : ", encoded)
	}

	// signature = hmac(i_key, data || iv), first 4 bytes
	sig := helpers.HmacSum(integrityFun, append(data[:], iv[:]...))[:4]
	copy(signature[:], sig[:])
	if isDebugMode == true {
		glog.Info("// signature = hmac(i_key, data || iv), first 4 bytes")
		glog.Info("Signature : ", sig)
	}

	glog.Flush()

	// final_message = WebSafeBase64Encode( iv || enc_price || signature )
	return base64.URLEncoding.EncodeToString(append(append(iv[:], encoded[:]...), signature[:]...)), err
}

func (dc *DoubleClickPricer) Decrypt(encryptedPrice string, isDebugMode bool) (float64, error) {
	var err error
	var errPrice float64

	encryptingFun, err := helpers.CreateHmac(dc.encryptionKey, dc.keyDecodingMode)
	if err != nil {
		return errPrice, nil
	}

	integrityFun, err := helpers.CreateHmac(dc.integrityKey, dc.keyDecodingMode)
	if err != nil {
		return errPrice, nil
	}

	if isDebugMode == true {
		glog.Info("Encryption key : ", dc.encryptionKey)
		glog.Info("Integrity key : ", dc.integrityKey)
	}

	// Decode base64
	encryptedPrice = helpers.AddBase64Padding(encryptedPrice)
	decoded, err := base64.URLEncoding.DecodeString(encryptedPrice)
	if err != nil {
		return errPrice, err
	}

	if isDebugMode == true {
		glog.Info("Encrypted price : ", encryptedPrice)
		glog.Info("Base64 decoded price : ", decoded)
	}

	// Get elements
	var (
		iv         [16]byte
		p          [8]byte
		signature  [4]byte
		priceMicro [8]byte
	)
	copy(iv[:], decoded[0:16])
	copy(p[:], decoded[16:24])
	copy(signature[:], decoded[24:28])

	// pad = hmac(e_key, iv)
	pad := helpers.HmacSum(encryptingFun, iv[:])[:8]

	if isDebugMode == true {
		glog.Info("IV : ", hex.EncodeToString(iv[:]))
		glog.Info("Encoded price : ", hex.EncodeToString(p[:]))
		glog.Info("Signature : ", hex.EncodeToString(signature[:]))
		glog.Info("Pad : ", hex.EncodeToString(pad[:]))
	}

	// priceMicro = p <xor> pad
	for i := range p {
		priceMicro[i] = pad[i] ^ p[i]
	}

	// conf_sig = hmac(i_key, data || iv)
	sig := helpers.HmacSum(integrityFun, append(priceMicro[:], iv[:]...))[:4]

	// success = (conf_sig == sig)
	for i := range sig {
		if sig[i] != signature[i] {
			return errPrice, errors.New("Failed to decrypt")
		}
	}
	price := float64(binary.BigEndian.Uint64(priceMicro[:])) / dc.scaleFactor

	glog.Flush()

	return price, err
}
