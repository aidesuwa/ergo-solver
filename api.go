package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

// apiClient handles HTTP communication with the puzzle API.
type apiClient struct {
	baseURL       string
	baseURLParsed *url.URL
	cookie        string
	userAgent     string
	jar           http.CookieJar
	http          *http.Client
}

// newAPIClient creates a new API client with the given configuration.
func newAPIClient(cfg appConfig) (*apiClient, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("base_url is required")
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base_url: %w", err)
	}
	u.Path = strings.TrimRight(u.Path, "/")

	jar, _ := cookiejar.New(nil)
	if jar != nil && strings.TrimSpace(cfg.Cookie) != "" {
		jar.SetCookies(u, parseCookieHeader(cfg.Cookie))
	}

	c := &apiClient{
		baseURL:       u.String(),
		baseURLParsed: u,
		cookie:        strings.TrimSpace(cfg.Cookie),
		userAgent:     cfg.UserAgent,
		jar:           jar,
		http: &http.Client{
			Timeout: 30 * time.Second,
			Jar:     jar,
		},
	}
	if c.userAgent == "" {
		c.userAgent = defaultUA
	}
	return c, nil
}

// apiError represents an HTTP error response from the API.
type apiError struct {
	StatusCode int
	Message    string
	Body       []byte
}

func (e *apiError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("api %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("api %d", e.StatusCode)
}

// doJSON performs an HTTP request with JSON body and response.
func (c *apiClient) doJSON(ctx context.Context, method, path string, body any, out any) error {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	reqURL := c.baseURL + path

	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		buf = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, buf)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Referer", c.baseURL+"/")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.jar == nil && c.cookie != "" {
		req.Header.Set("Cookie", c.cookie)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	const maxResponseSize = 10 * 1024 * 1024 // 10MB limit
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	c.cookie = strings.TrimSpace(c.exportCookieHeader())

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := ""
		var m map[string]any
		if json.Unmarshal(b, &m) == nil {
			if s, ok := m["message"].(string); ok && s != "" {
				msg = s
			} else if s, ok := m["error"].(string); ok && s != "" {
				msg = s
			}
		}
		return &apiError{StatusCode: resp.StatusCode, Message: msg, Body: b}
	}

	if out == nil {
		return nil
	}
	if len(b) == 0 {
		return errors.New("empty response body")
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	return nil
}

// exportCookieHeader returns the current cookies as a header string.
func (c *apiClient) exportCookieHeader() string {
	if c.jar == nil || c.baseURLParsed == nil {
		return strings.TrimSpace(c.cookie)
	}
	cookies := c.jar.Cookies(c.baseURLParsed)
	if len(cookies) == 0 {
		return strings.TrimSpace(c.cookie)
	}
	pairs := make([]string, 0, len(cookies))
	for _, ck := range cookies {
		if ck == nil || strings.TrimSpace(ck.Name) == "" {
			continue
		}
		pairs = append(pairs, ck.Name+"="+ck.Value)
	}
	return strings.Join(pairs, "; ")
}

// parseCookieHeader parses a Cookie header string into individual cookies.
func parseCookieHeader(header string) []*http.Cookie {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil
	}
	parts := strings.Split(header, ";")
	out := make([]*http.Cookie, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			continue
		}
		name := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		if name == "" {
			continue
		}
		out = append(out, &http.Cookie{Name: name, Value: val, Path: "/"})
	}
	return out
}

// authMeResponse represents the /api/auth/me response.
type authMeResponse struct {
	User struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"user"`
}

// authMe fetches the current authenticated user info.
func (c *apiClient) authMe(ctx context.Context) (*authMeResponse, error) {
	var out authMeResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/auth/me", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// dailyRemainingResponse represents the daily puzzle limit status.
type dailyRemainingResponse struct {
	Remaining int `json:"remaining"`
	Completed int `json:"completed"`
	Limit     int `json:"limit"`
}

// dailyRemaining fetches the remaining daily puzzle attempts.
func (c *apiClient) dailyRemaining(ctx context.Context) (*dailyRemainingResponse, error) {
	var out dailyRemainingResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/daily/remaining", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// powStatusResponse represents the PoW status.
type powStatusResponse struct {
	HasValidPow         bool  `json:"hasValidPow"`
	PowExpiresAt        int64 `json:"powExpiresAt"`
	HasOngoingChallenge bool  `json:"hasOngoingChallenge"`
}

// powStatus fetches the current PoW status.
func (c *apiClient) powStatus(ctx context.Context) (*powStatusResponse, error) {
	var out powStatusResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/pow/status", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// powChallengeResponse represents a new PoW challenge.
type powChallengeResponse struct {
	Challenge  string `json:"challenge"`
	Difficulty int    `json:"difficulty"`
	ExpiresAt  int64  `json:"expiresAt"`
}

// powChallenge requests a new PoW challenge.
func (c *apiClient) powChallenge(ctx context.Context) (*powChallengeResponse, error) {
	var out powChallengeResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/pow/challenge", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// powVerifyRequest represents the PoW verification request body.
type powVerifyRequest struct {
	Challenge string `json:"challenge"`
	Nonce     string `json:"nonce"`
}

// powVerify submits a PoW solution for verification.
func (c *apiClient) powVerify(ctx context.Context, challenge, nonce string) error {
	var out map[string]any
	return c.doJSON(ctx, http.MethodPost, "/api/pow/verify", powVerifyRequest{Challenge: challenge, Nonce: nonce}, &out)
}

// puzzleExample represents a training example with input/output grids.
type puzzleExample struct {
	Input  [][]int `json:"input"`
	Output [][]int `json:"output"`
}

// puzzleHints contains hints about the expected answer.
type puzzleHints struct {
	BackgroundColor int `json:"backgroundColor"`
	AnswerSize      struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"answerSize"`
}

// puzzle represents an ARC puzzle with training examples and test input.
type puzzle struct {
	ID        string          `json:"id"`
	Train     []puzzleExample `json:"train"`
	TestInput [][]int         `json:"testInput"`
	Hints     puzzleHints     `json:"hints"`
}

// puzzleNewResponse represents the response when fetching a new puzzle.
type puzzleNewResponse struct {
	Puzzle            puzzle `json:"puzzle"`
	RemainingAttempts int    `json:"remainingAttempts"`
	DailyRemaining    int    `json:"dailyRemaining"`
	DailyLimit        int    `json:"dailyLimit"`
}

// puzzleNew fetches a new puzzle to solve.
func (c *apiClient) puzzleNew(ctx context.Context) (*puzzleNewResponse, error) {
	var out puzzleNewResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/puzzle/new", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// puzzleSubmitRequest represents the answer submission request.
type puzzleSubmitRequest struct {
	PuzzleID string  `json:"puzzleId"`
	Answer   [][]int `json:"answer"`
}

// puzzleSubmitResponse represents the answer submission result.
type puzzleSubmitResponse struct {
	Success           bool   `json:"success"`
	Correct           bool   `json:"correct"`
	Message           string `json:"message"`
	RemainingAttempts int    `json:"remainingAttempts"`
	PointsAwarded     int    `json:"pointsAwarded"`
	PointsBalance     int    `json:"pointsBalance"`
	DailyRemaining    int    `json:"dailyRemaining"`
	DailyLimit        int    `json:"dailyLimit"`
}

// puzzleSubmit submits an answer for the given puzzle.
func (c *apiClient) puzzleSubmit(ctx context.Context, puzzleID string, answer [][]int) (*puzzleSubmitResponse, error) {
	var out puzzleSubmitResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/puzzle/submit", puzzleSubmitRequest{PuzzleID: puzzleID, Answer: answer}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
