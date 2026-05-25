package services

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"math"
)

// spotifyTOTPConfig holds the static configuration for Spotify's TOTP scheme.
var spotifyTOTPConfig = struct {
	version  string
	interval int64
	digits   int
	cipher   []int
}{
	version:  "61",
	interval: 30,
	digits:   6,
	cipher: []int{
		44, 55, 47, 42, 70, 40, 34, 114, 76, 74,
		50, 111, 120, 97, 75, 76, 94, 102, 43, 69,
		49, 120, 118, 80, 64, 78,
	},
}

// generateSpotifyTOTP generates the TOTP code required by Spotify's token endpoint.
//
// Spotify's /api/token endpoint requires a time-based one-time password derived
// from a fixed cipher. The algorithm is standard RFC 6238 HMAC-SHA1 TOTP with
// Spotify-specific secret derivation:
//
//  1. XOR each cipher byte with positional key: (index % 33) + 9
//  2. Concatenate string representations of the transformed integers
//  3. Hex-encode the concatenated string's UTF-8 bytes
//  4. Decode the hex string to raw bytes — these are the HMAC secret
//  5. Standard HMAC-SHA1 TOTP with 6 digits and 30-second interval
func generateSpotifyTOTP(serverTimeSeconds int64) string {
	secret := deriveSpotifySecret()
	return computeTOTP(secret, serverTimeSeconds)
}

func deriveSpotifySecret() []byte {
	// Step 1: XOR transform each cipher byte with its positional key
	transformed := make([]int, len(spotifyTOTPConfig.cipher))
	for i, b := range spotifyTOTPConfig.cipher {
		transformed[i] = b ^ ((i % 33) + 9)
	}

	// Step 2: Concatenate string representations
	joined := ""
	for _, v := range transformed {
		joined += fmt.Sprintf("%d", v)
	}

	// Step 3: Hex-encode the UTF-8 bytes
	hexStr := ""
	for _, b := range []byte(joined) {
		hexStr += fmt.Sprintf("%02x", b)
	}

	// Step 4: Decode hex string to raw bytes — this IS the HMAC secret
	return hexStringToBytes(hexStr)
}

func computeTOTP(secret []byte, timeSeconds int64) string {
	counter := timeSeconds / spotifyTOTPConfig.interval
	counterBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(counterBytes, uint64(counter))

	mac := hmac.New(sha1.New, secret)
	mac.Write(counterBytes)
	hash := mac.Sum(nil)

	// Dynamic truncation per RFC 4226
	offset := hash[len(hash)-1] & 0x0F
	binCode := (int(hash[offset]&0x7F) << 24) |
		(int(hash[offset+1]&0xFF) << 16) |
		(int(hash[offset+2]&0xFF) << 8) |
		int(hash[offset+3]&0xFF)

	otp := binCode % int(math.Pow10(spotifyTOTPConfig.digits))
	return fmt.Sprintf("%0*d", spotifyTOTPConfig.digits, otp)
}

func hexStringToBytes(hex string) []byte {
	data := make([]byte, len(hex)/2)
	for i := 0; i < len(hex); i += 2 {
		high := hexCharToNibble(hex[i])
		low := hexCharToNibble(hex[i+1])
		data[i/2] = (high << 4) | low
	}
	return data
}

func hexCharToNibble(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	default:
		return 0
	}
}
