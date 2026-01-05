package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"
	"time"
)

// powRefreshWindow is the time before expiry to refresh PoW.
const powRefreshWindow = 120 * time.Second

// ensurePow checks PoW status and refreshes if needed.
func ensurePow(ctx context.Context, c *apiClient, log *logger) error {
	st, err := c.powStatus(ctx)
	if err != nil {
		return err
	}

	now := time.Now()
	exp := time.UnixMilli(st.PowExpiresAt)

	need := !st.HasValidPow
	if st.HasValidPow && st.PowExpiresAt > 0 && exp.Sub(now) < powRefreshWindow {
		need = true
	}

	if !need {
		log.ok("PoW valid, no refresh needed")
		return nil
	}

	log.info("PoW needs refresh, solving...")
	chal, err := c.powChallenge(ctx)
	if err != nil {
		return err
	}

	start := time.Now()
	nonce, err := computePowNonce(ctx, chal.Challenge, chal.Difficulty, log)
	if err != nil {
		return err
	}
	elapsed := time.Since(start)

	log.okf("PoW found nonce=%s (elapsed %s)", nonce, elapsed.Round(10*time.Millisecond))

	if err := c.powVerify(ctx, chal.Challenge, nonce); err != nil {
		return err
	}
	log.ok("PoW verified")
	return nil
}

// computePowNonce finds a nonce where sha256(challenge+nonce) has the required
// number of leading zero nibbles (hex digits).
func computePowNonce(ctx context.Context, challenge string, difficulty int, log *logger) (string, error) {
	if difficulty < 0 || difficulty > 64 {
		return "", fmt.Errorf("invalid difficulty: %d", difficulty)
	}

	fullZeroBytes := difficulty / 2
	halfNibble := difficulty%2 == 1

	start := time.Now()
	nextLogAt := start.Add(2 * time.Second)
	const checkEvery = 100_000

	for i := 0; ; i++ {
		if i%checkEvery == 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			default:
			}
		}

		nonce := strconv.Itoa(i)
		sum := sha256.Sum256([]byte(challenge + nonce))
		if hasLeadingZeroNibbles(sum, fullZeroBytes, halfNibble) {
			return nonce, nil
		}

		if log != nil && i%checkEvery == 0 {
			now := time.Now()
			if now.Before(nextLogAt) {
				continue
			}
			attempts := i + 1
			elapsed := now.Sub(start)
			rate := float64(attempts) / elapsed.Seconds()
			log.infof("PoW in progress: difficulty=%d attempts=%d rate=%.0f/s elapsed=%s", difficulty, attempts, rate, elapsed.Round(100*time.Millisecond))
			nextLogAt = now.Add(2 * time.Second)
		}
	}
}

// hasLeadingZeroNibbles checks if hash has the required leading zeros.
func hasLeadingZeroNibbles(sum [sha256.Size]byte, fullZeroBytes int, halfNibble bool) bool {
	for i := 0; i < fullZeroBytes; i++ {
		if sum[i] != 0 {
			return false
		}
	}
	if halfNibble {
		return sum[fullZeroBytes]&0xF0 == 0
	}
	return true
}
