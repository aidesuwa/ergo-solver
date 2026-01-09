package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Command names.
const (
	cmdSolve = "solve"
	cmdHelp  = "help"
)

// errAuthRequired indicates authentication is needed.
var errAuthRequired = errors.New("auth_required")

func main() {
	_ = godotenv.Load()
	log := newLogger()
	if err := run(context.Background(), log, os.Args[1:]); err != nil {
		log.err(err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context, log *logger, args []string) error {
	if len(args) == 0 {
		printUsage(os.Stdout)
		return nil
	}

	switch args[0] {
	case cmdHelp, "-h", "--help":
		printUsage(os.Stdout)
		return nil
	case cmdSolve:
		return runSolve(ctx, log, args[1:])
	default:
		printUsage(os.Stderr)
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "ergo-solver: ARC puzzle solver CLI")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(w, "  ergo-solver solve --config PATH [--count N] [--dry-run] [--auto]")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Options:")
	_, _ = fmt.Fprintln(w, "  --config  Path to config.json (required)")
	_, _ = fmt.Fprintln(w, "  --count   Number of puzzles to solve (default: 1)")
	_, _ = fmt.Fprintln(w, "  --dry-run Solve but do not submit")
	_, _ = fmt.Fprintln(w, "  --auto    Auto-loop until daily limit exhausted (1-5 min interval)")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Environment:")
	_, _ = fmt.Fprintln(w, "  NO_COLOR  Disable colored output")
}

func runSolve(ctx context.Context, log *logger, args []string) error {
	fs := flag.NewFlagSet(cmdSolve, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		configPath string
		count      int
		dryRun     bool
		autoLoop   bool
	)
	fs.StringVar(&configPath, "config", "", "config path (required)")
	fs.IntVar(&count, "count", 1, "how many puzzles to solve per round")
	fs.BoolVar(&dryRun, "dry-run", false, "solve but do not submit")
	fs.BoolVar(&autoLoop, "auto", false, "auto loop until daily limit exhausted")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if configPath == "" {
		return fmt.Errorf("--config is required")
	}
	if count <= 0 {
		return fmt.Errorf("--count must be > 0")
	}

	log.infof("starting: count=%d dryRun=%v autoLoop=%v", count, dryRun, autoLoop)

	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}

	cfg, err = ensureLoginInteractive(ctx, cfg, configPath, log)
	if err != nil {
		return err
	}

	client, err := newAPIClient(cfg)
	if err != nil {
		return err
	}
	me, err := client.authMe(ctx)
	if err != nil {
		if isAuthError(err) {
			return errAuthRequired
		}
		return err
	}
	_ = persistCookieIfChanged(configPath, &cfg, client, log)
	log.okf("logged in: %s(%s)", me.User.Username, me.User.ID)
	log.infof("site: %s", cfg.BaseURL)

	if dr, err := client.dailyRemaining(ctx); err == nil {
		log.infof("daily quota: remaining=%d completed=%d limit=%d", dr.Remaining, dr.Completed, dr.Limit)
		if dr.Remaining <= 0 {
			log.warn("stopping: daily limit exhausted")
			return nil
		}
	} else {
		log.warnf("failed to query daily quota: %s (will try fetching puzzle)", err.Error())
	}

	if err := ensurePow(ctx, client, log); err != nil {
		return err
	}
	_ = persistCookieIfChanged(configPath, &cfg, client, log)

	solver, err := newAISolver(ctx, cfg, log)
	if err != nil {
		return err
	}
	if solver == nil {
		return errors.New("AI solver not configured")
	}

	solvedCount := 0
	startAll := time.Now()
	for solvedCount < count {
		log.infof("fetching puzzle: index=%d/%d", solvedCount+1, count)
		pNew, err := puzzleNewWithRetry(ctx, client, log)
		if err != nil {
			if isDailyExhaustedError(err) {
				log.warn("stopping: daily limit exhausted")
				return nil
			}
			if isAuthError(err) {
				log.warn("auth expired, re-authenticating...")
				cfg, err = ensureLoginInteractive(ctx, cfg, configPath, log)
				if err != nil {
					return err
				}
				client, err = newAPIClient(cfg)
				if err != nil {
					return err
				}
				continue
			}
			return err
		}
		_ = persistCookieIfChanged(configPath, &cfg, client, log)

		if pNew.DailyRemaining <= 0 {
			log.warn("stopping: daily limit exhausted")
			return nil
		}

		log.infof("puzzle fetched: puzzleId=%s, remainingAttempts=%d, dailyRemaining=%d/%d", pNew.Puzzle.ID, pNew.RemainingAttempts, pNew.DailyRemaining, pNew.DailyLimit)

		start := time.Now()
		answer, err := solver.Solve(ctx, pNew.Puzzle)
		if err != nil {
			if errors.Is(err, ErrAIUnavailable) {
				log.err("AI service unavailable")
				return fmt.Errorf("AI unavailable: %w", err)
			}
			if autoLoop {
				log.warnf("AI solve failed: %v, skipping...", err)
				waitDur := time.Duration(30+rand.Intn(30)) * time.Second
				log.infof("sleeping %s before continue...", waitDur.Round(time.Second))
				time.Sleep(waitDur)
				count = solvedCount + 1
				continue
			}
			return fmt.Errorf("ai solve failed: %w", err)
		}
		log.okf("AI solved (elapsed %s)", time.Since(start).Round(10*time.Millisecond))

		if dryRun {
			log.okf("dry-run: puzzleId=%s answer generated but not submitted", pNew.Puzzle.ID)
			solvedCount++
			continue
		}

		if err := ensurePow(ctx, client, log); err != nil {
			return err
		}
		_ = persistCookieIfChanged(configPath, &cfg, client, log)

		log.infof("submitting: puzzleId=%s", pNew.Puzzle.ID)
		sub, err := submitWithRetry(ctx, client, log, pNew.Puzzle.ID, answer)
		if err != nil {
			if isAuthError(err) {
				log.warn("auth expired, re-authenticating...")
				cfg, err = ensureLoginInteractive(ctx, cfg, configPath, log)
				if err != nil {
					return err
				}
				client, err = newAPIClient(cfg)
				if err != nil {
					return err
				}
				continue
			}
			return err
		}
		_ = persistCookieIfChanged(configPath, &cfg, client, log)

		if !sub.Success {
			return fmt.Errorf("submit failed: %s", sub.Message)
		}

		log.infof("submit response: %s", sub.Message)
		if sub.Correct {
			log.okf("correct: +%d points, balance=%d, dailyRemaining=%d/%d", sub.PointsAwarded, sub.PointsBalance, sub.DailyRemaining, sub.DailyLimit)
			solvedCount++

			if autoLoop && sub.DailyRemaining > 0 {
				waitMin := 1*60 + rand.Intn(4*60+1) // 60-300s
				waitDur := time.Duration(waitMin) * time.Second
				log.infof("auto mode: sleeping %s (remaining %d)...", waitDur.Round(time.Second), sub.DailyRemaining)
				time.Sleep(waitDur)
				count = solvedCount + 1
			}
			continue
		}
		log.warnf("incorrect: remainingAttempts=%d", sub.RemainingAttempts)
		if autoLoop {
			log.warn("auto mode: answer incorrect, skipping...")
			waitDur := time.Duration(30+rand.Intn(30)) * time.Second
			log.infof("sleeping %s before continue...", waitDur.Round(time.Second))
			time.Sleep(waitDur)
			count = solvedCount + 1
			continue
		}
		return errors.New("submitted answer was incorrect")
	}

	if autoLoop {
		log.okf("auto mode complete: daily limit exhausted, solved %d puzzles, elapsed %s", solvedCount, time.Since(startAll).Round(time.Second))
		return nil
	}

	log.okf("done: solved=%d/%d elapsed=%s", solvedCount, count, time.Since(startAll).Round(100*time.Millisecond))
	return nil
}

// persistCookieIfChanged saves config if cookies have been updated.
func persistCookieIfChanged(configPath string, cfg *appConfig, c *apiClient, log *logger) error {
	if cfg == nil || c == nil {
		return nil
	}
	newCookie := strings.TrimSpace(c.exportCookieHeader())
	if newCookie == "" {
		return nil
	}
	if strings.TrimSpace(cfg.Cookie) == newCookie {
		return nil
	}
	cfg.Cookie = newCookie
	if err := saveConfig(configPath, *cfg); err != nil {
		return err
	}
	if log != nil {
		log.ok("config.json updated (cookie refreshed)")
	}
	return nil
}

func puzzleNewWithRetry(ctx context.Context, client *apiClient, log *logger) (*puzzleNewResponse, error) {
	backoff := 2 * time.Second
	for {
		pNew, err := client.puzzleNew(ctx)
		if err == nil {
			return pNew, nil
		}
		var ae *apiError
		if errors.As(err, &ae) && ae.StatusCode == 429 {
			log.warnf("rate limited (429), waiting %s...", backoff.Round(100*time.Millisecond))
			time.Sleep(backoff)
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		return nil, err
	}
}

func submitWithRetry(ctx context.Context, client *apiClient, log *logger, puzzleID string, answer [][]int) (*puzzleSubmitResponse, error) {
	backoff := 2 * time.Second
	for {
		sub, err := client.puzzleSubmit(ctx, puzzleID, answer)
		if err == nil {
			return sub, nil
		}
		var ae *apiError
		if errors.As(err, &ae) && ae.StatusCode == 429 {
			log.warnf("submit rate limited (429), waiting %s...", backoff.Round(100*time.Millisecond))
			time.Sleep(backoff)
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		return nil, err
	}
}

func ensureLoginInteractive(ctx context.Context, cfg appConfig, configPath string, log *logger) (appConfig, error) {
	cfg.Cookie = strings.TrimSpace(cfg.Cookie)
	if cfg.Cookie == "" {
		in, err := promptAuthMaterial()
		if err != nil {
			return appConfig{}, err
		}
		cfg.Cookie = in.Cookie
		if in.UserAgent != "" {
			cfg.UserAgent = in.UserAgent
		}
		if in.BaseURL != "" && cfg.BaseURL == "" {
			cfg.BaseURL = in.BaseURL
		}
		if err := saveConfig(configPath, cfg); err != nil {
			return appConfig{}, err
		}
		log.ok("config.json updated (cookie saved)")
	}

	client, err := newAPIClient(cfg)
	if err != nil {
		return appConfig{}, err
	}
	if _, err := client.authMe(ctx); err != nil {
		if !isAuthError(err) {
			return appConfig{}, err
		}

		in, perr := promptAuthMaterial()
		if perr != nil {
			return appConfig{}, perr
		}
		cfg.Cookie = in.Cookie
		if in.UserAgent != "" {
			cfg.UserAgent = in.UserAgent
		}
		if in.BaseURL != "" && cfg.BaseURL == "" {
			cfg.BaseURL = in.BaseURL
		}
		if err := saveConfig(configPath, cfg); err != nil {
			return appConfig{}, err
		}
		log.ok("config.json updated (cookie saved)")

		client, err = newAPIClient(cfg)
		if err != nil {
			return appConfig{}, err
		}
		if _, err := client.authMe(ctx); err != nil {
			if isAuthError(err) {
				return appConfig{}, errors.New("login still invalid: please check cookie/token")
			}
			return appConfig{}, err
		}
	}
	return cfg, nil
}

// authMaterial holds parsed authentication data from user input.
type authMaterial struct {
	Cookie    string
	UserAgent string
	BaseURL   string
}

func promptAuthMaterial() (authMaterial, error) {
	_, _ = fmt.Fprintln(os.Stdout, "Enter token/cookie (paste cookie / `Cookie: ...` / curl command, end with empty line):")
	_, _ = fmt.Fprint(os.Stdout, "> ")

	var lines []string
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			break
		}
		lines = append(lines, line)
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return authMaterial{}, err
	}
	text := strings.TrimSpace(strings.Join(lines, "\n"))
	if text == "" {
		return authMaterial{}, errors.New("empty input")
	}

	out := parseAuthMaterial(text)
	if out.Cookie == "" {
		return authMaterial{}, errors.New("cookie not found: paste `-b '...'` content or `Cookie: ...`")
	}
	return out, nil
}

// Regex patterns for parsing curl commands and headers.
var (
	reCurlCookieBQuoted   = regexp.MustCompile(`(?s)(?:^|\s)-b\s+(?:'([^']*)'|"([^"]*)")`)
	reCurlCookieBUnquoted = regexp.MustCompile(`(?m)(?:^|\s)-b\s+([^\s\\]+)`)
	reHeaderCookie        = regexp.MustCompile(`(?im)^\s*(?:-H\s+)?['"]?cookie\s*:\s*(.*?)['"]?\s*$`)
	reHeaderUA            = regexp.MustCompile(`(?im)^\s*(?:-H\s+)?['"]?user-agent\s*:\s*(.*?)['"]?\s*$`)
	reAnyURL              = regexp.MustCompile(`https?://[^\s'"\\]+`)
)

func parseAuthMaterial(text string) authMaterial {
	var out authMaterial
	trim := func(s string) string { return strings.TrimSpace(strings.Trim(s, `"'`)) }

	if m := reCurlCookieBQuoted.FindStringSubmatch(text); len(m) == 3 {
		out.Cookie = trim(m[1])
		if out.Cookie == "" {
			out.Cookie = trim(m[2])
		}
	}
	if out.Cookie == "" {
		if m := reHeaderCookie.FindStringSubmatch(text); len(m) == 2 {
			out.Cookie = trim(m[1])
		}
	}
	if out.Cookie == "" {
		if m := reCurlCookieBUnquoted.FindStringSubmatch(text); len(m) == 2 {
			out.Cookie = trim(m[1])
		}
	}

	if m := reHeaderUA.FindStringSubmatch(text); len(m) == 2 {
		out.UserAgent = trim(m[1])
	}

	if m := reAnyURL.FindString(text); m != "" {
		if u, err := urlToBase(m); err == nil && u != "" {
			out.BaseURL = u
		}
	}

	if out.Cookie == "" {
		line := strings.TrimSpace(text)
		line = strings.TrimPrefix(line, "Cookie:")
		line = strings.TrimSpace(line)
		out.Cookie = trim(line)
	}
	return out
}

func urlToBase(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", errors.New("invalid url")
	}
	return u.Scheme + "://" + u.Host, nil
}

func isAuthError(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && (ae.StatusCode == 401 || ae.StatusCode == 403)
}

// isDailyExhaustedError checks if the error indicates daily limit is reached.
// Note: Chinese strings match server-side error messages.
func isDailyExhaustedError(err error) bool {
	var ae *apiError
	if !errors.As(err, &ae) || ae.StatusCode != 403 {
		return false
	}
	msg := strings.TrimSpace(ae.Message)
	return strings.Contains(msg, "次数已用完") || // "quota exhausted"
		strings.Contains(msg, "已完成") || // "completed"
		strings.Contains(msg, "请明天再来") // "come back tomorrow"
}
