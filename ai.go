package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

// ErrAIUnavailable indicates the AI service is not reachable or returned an error.
var ErrAIUnavailable = errors.New("AI service unavailable")

// ANSI color codes for terminal output.
const (
	colorReset  = "\033[0m"
	colorCyan   = "\033[36m"
	colorYellow = "\033[33m"
	colorGreen  = "\033[32m"
	colorBlue   = "\033[34m"
	colorDim    = "\033[2m"
)

// spinner provides a terminal loading animation.
type spinner struct {
	mu      sync.Mutex
	active  bool
	stop    chan struct{}
	done    chan struct{}
	message string
	frames  []string
	start   time.Time
	isTTY   bool
}

func newSpinner() *spinner {
	isTTY := false
	if fi, err := os.Stdout.Stat(); err == nil {
		isTTY = (fi.Mode() & os.ModeCharDevice) != 0
	}
	return &spinner{
		frames: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		isTTY:  isTTY,
	}
}

func (s *spinner) Start(msg string) {
	s.mu.Lock()
	if s.active {
		s.mu.Unlock()
		return
	}
	s.active = true
	s.message = msg
	s.start = time.Now()
	s.mu.Unlock()

	if !s.isTTY {
		fmt.Printf("%s %s\n", colorCyan+"⋯"+colorReset, msg)
		return
	}

	s.stop = make(chan struct{})
	s.done = make(chan struct{})

	go func() {
		defer close(s.done)
		i := 0
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-ticker.C:
				s.mu.Lock()
				elapsed := time.Since(s.start).Round(100 * time.Millisecond)
				fmt.Printf("\r%s%s %s%s %s[%s]%s  ", colorCyan, s.frames[i%len(s.frames)], s.message, colorReset, colorDim, elapsed, colorReset)
				s.mu.Unlock()
				i++
			}
		}
	}()
}

func (s *spinner) Stop() {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return
	}
	s.active = false
	stopCh := s.stop
	doneCh := s.done
	s.mu.Unlock()

	if s.isTTY && stopCh != nil {
		close(stopCh)
		<-doneCh
		fmt.Print("\r\033[K")
	}
}

// Solver uses an OpenAI-compatible API to solve ARC puzzles.
type Solver struct {
	client openai.Client
	model  string
	cfg    aiConfig
	log    *logger
}

// Answer represents the structured response from the AI solver.
type Answer struct {
	Reasoning  string  `json:"reasoning"`
	Answer     [][]int `json:"answer"`
	Confidence int     `json:"confidence"`
}

// VerifyResult represents the AI verification response.
type VerifyResult struct {
	Valid     bool   `json:"valid"`
	Reasoning string `json:"reasoning"`
}

// JSON Schema for AI answer output.
var arcAnswerSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"reasoning": map[string]any{
			"type":        "string",
			"description": "Step-by-step reasoning about the transformation pattern",
		},
		"answer": map[string]any{
			"type":        "array",
			"description": "2D array representing the output grid",
			"items": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "integer",
				},
			},
		},
		"confidence": map[string]any{
			"type":        "integer",
			"description": "Confidence level 0-100",
		},
	},
	"required":             []string{"reasoning", "answer", "confidence"},
	"additionalProperties": false,
}

// JSON Schema for verification response.
var verifySchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"valid": map[string]any{
			"type":        "boolean",
			"description": "Whether the answer is valid",
		},
		"reasoning": map[string]any{
			"type":        "string",
			"description": "Explanation of why the answer is valid or invalid",
		},
	},
	"required":             []string{"valid", "reasoning"},
	"additionalProperties": false,
}

func newAISolver(ctx context.Context, cfg appConfig, log *logger) (*Solver, error) {
	if !cfg.AI.Enabled {
		return nil, nil
	}

	apiKey := strings.TrimSpace(cfg.AI.APIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	}
	if apiKey == "" {
		return nil, errors.New("missing API key (set ai.api_key in config or OPENAI_API_KEY env)")
	}

	modelName := strings.TrimSpace(cfg.AI.Model)
	if modelName == "" {
		modelName = defaultAIModel
	}

	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithHeader("User-Agent", "curl/8.0"),
	}

	if baseURL := strings.TrimSpace(cfg.AI.BaseURL); baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
		log.infof("AI using custom endpoint: %s", baseURL)
	}

	client := openai.NewClient(opts...)
	return &Solver{client: client, model: modelName, cfg: cfg.AI, log: log}, nil
}

const systemPrompt = `You are an expert ARC (Abstraction and Reasoning Corpus) puzzle solver.

## Solving Strategy (follow these steps):
1. **Object Detection**: Identify distinct objects (connected regions of same color)
2. **Pattern Recognition**: Compare ALL training input→output pairs to find the rule
3. **Rule Formulation**: Express the transformation as a combination of primitives
4. **Apply Rule**: Apply the exact same rule to test_input
5. **Verify Dimensions**: Ensure output matches expected size EXACTLY

## Common ARC Primitives (DSL operations):
- **Color ops**: recolor, swap colors, fill, flood fill
- **Geometry**: rotate(90/180/270), flip(h/v), translate, scale(2x/3x)
- **Object ops**: copy, move, delete, duplicate, mirror
- **Grid ops**: crop, extend, tile, overlay, mask
- **Pattern ops**: repeat, symmetry, count, sort by size/color
- **Spatial**: align, stack, frame, border, connect points

## Output Format (MUST be ONLY valid JSON, no other text):
{
  "reasoning": "Step 1: [object detection]... Step 2: [pattern found]... Step 3: [rule]...",
  "answer": [[1,2,3],[4,5,6],[7,8,9]],
  "confidence": 95
}

## CRITICAL Requirements:
- Output ONLY the JSON object, no markdown, no explanation outside JSON
- answer MUST be a 2D array with EXACTLY the dimensions specified in hints
- EVERY row MUST have IDENTICAL length (the expected width)
- Count your rows and columns before outputting to verify dimensions
- confidence: 0-100, only >= 90 if you're certain about the pattern`

// Solve attempts to solve the given puzzle using AI.
func (s *Solver) Solve(ctx context.Context, p puzzle) ([][]int, error) {
	puzzleJSON, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal puzzle: %w", err)
	}

	userQuery := fmt.Sprintf(`Solve this ARC puzzle:

%s

IMPORTANT: Expected answer dimensions are EXACTLY %d rows × %d columns.
Your answer array MUST have exactly %d rows, and EACH row MUST have exactly %d elements.
Double-check your dimensions before responding!`, string(puzzleJSON), p.Hints.AnswerSize.Height, p.Hints.AnswerSize.Width, p.Hints.AnswerSize.Height, p.Hints.AnswerSize.Width)

	fmt.Println()
	fmt.Printf("%s┌─────────────────────────────────────────┐%s\n", colorCyan, colorReset)
	fmt.Printf("%s│      🤖 AI Agent Starting                │%s\n", colorCyan, colorReset)
	fmt.Printf("%s│      📦 Model: %-24s│%s\n", colorCyan, s.model, colorReset)
	fmt.Printf("%s└─────────────────────────────────────────┘%s\n", colorCyan, colorReset)
	fmt.Println()

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(systemPrompt),
		openai.UserMessage(userQuery),
	}

	spin := newSpinner()
	spin.Start("🔍 Analyzing puzzle...")

	stream := s.client.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(s.model),
		Messages: messages,
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
				JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:        "arc_answer",
					Description: openai.String("ARC puzzle answer with reasoning"),
					Strict:      openai.Bool(true),
					Schema:      arcAnswerSchema,
				},
			},
		},
	})

	var contentBuilder strings.Builder
	for stream.Next() {
		chunk := stream.Current()
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			contentBuilder.WriteString(chunk.Choices[0].Delta.Content)
		}
	}

	spin.Stop()

	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAIUnavailable, err)
	}

	content := contentBuilder.String()
	if content == "" {
		return nil, errors.New("no content in response")
	}

	var answer Answer
	if err := json.Unmarshal([]byte(content), &answer); err != nil {
		grid, parseErr := parseAnswerGrid(content)
		if parseErr != nil {
			return nil, parseErr
		}
		return grid, nil
	}

	if answer.Reasoning != "" {
		fmt.Printf("%s💭 AI Reasoning:%s\n", colorYellow, colorReset)
		fmt.Println(strings.Repeat("─", 50))
		fmt.Printf("%s%s%s\n", colorBlue, answer.Reasoning, colorReset)
		fmt.Println(strings.Repeat("─", 50))
	}

	fmt.Printf("%s📊 Confidence: %d%%%s\n", colorGreen, answer.Confidence, colorReset)

	if len(answer.Answer) == 0 {
		return nil, errors.New("empty answer grid")
	}

	if err := validateAnswerSize(p, answer.Answer); err != nil {
		s.log.warnf("answer size mismatch: %v", err)
	}

	spin2 := newSpinner()
	spin2.Start("🔄 AI self-verifying...")

	verified, verifyErr := s.verifyAnswer(ctx, p, answer.Answer)
	spin2.Stop()

	if verifyErr != nil {
		s.log.warnf("verification error: %v", verifyErr)
	} else if !verified {
		return nil, errors.New("AI self-verification failed: answer does not match pattern")
	}

	fmt.Printf("%s✅ AI self-verification passed!%s\n", colorGreen, colorReset)
	fmt.Printf("%s✨ Answer generated!%s\n", colorGreen, colorReset)

	return answer.Answer, nil
}

func parseAnswerGrid(text string) ([][]int, error) {
	var grid [][]int
	if err := json.Unmarshal([]byte(text), &grid); err == nil {
		return normalizeGrid(grid)
	}

	start := strings.Index(text, "[[")
	if start == -1 {
		start = strings.Index(text, "[")
	}
	if start == -1 {
		return nil, errors.New("not valid json array")
	}

	end := findMatchingBracket(text, start)
	if end == -1 || end <= start {
		return nil, errors.New("not valid json array")
	}
	candidate := text[start : end+1]
	if err := json.Unmarshal([]byte(candidate), &grid); err != nil {
		return nil, fmt.Errorf("parse json array: %w", err)
	}
	return normalizeGrid(grid)
}

func findMatchingBracket(text string, start int) int {
	depth := 0
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func normalizeGrid(grid [][]int) ([][]int, error) {
	if len(grid) == 0 {
		return nil, errors.New("empty grid")
	}

	maxWidth := 0
	for _, row := range grid {
		if len(row) > maxWidth {
			maxWidth = len(row)
		}
	}
	if maxWidth == 0 {
		return nil, errors.New("empty rows")
	}

	for i := range grid {
		if len(grid[i]) < maxWidth {
			padded := make([]int, maxWidth)
			copy(padded, grid[i])
			grid[i] = padded
		}
	}

	return grid, nil
}

func validateAnswerSize(p puzzle, grid [][]int) error {
	h := p.Hints.AnswerSize.Height
	w := p.Hints.AnswerSize.Width
	if h <= 0 || w <= 0 {
		return nil
	}
	if len(grid) != h {
		return fmt.Errorf("row count mismatch: got %d, want %d", len(grid), h)
	}
	for i, row := range grid {
		if len(row) != w {
			return fmt.Errorf("row %d width mismatch: got %d, want %d", i, len(row), w)
		}
	}
	return nil
}

const verifyPrompt = `You are an ARC puzzle validator. Your task is to verify if a proposed answer correctly follows the transformation pattern.

## Instructions:
1. Analyze the training examples to understand the transformation rule
2. Check if applying the SAME rule to test_input would produce the proposed_answer
3. Be STRICT - only return valid=true if you are confident the answer is correct

## Output Format (MUST be valid JSON):
{
  "valid": true/false,
  "reasoning": "Brief explanation of why the answer is correct or incorrect"
}

IMPORTANT: Return valid=true ONLY if the answer correctly follows the pattern. When in doubt, return false.`

func (s *Solver) verifyAnswer(ctx context.Context, p puzzle, answer [][]int) (bool, error) {
	puzzleJSON, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return false, fmt.Errorf("marshal puzzle: %w", err)
	}

	answerJSON, err := json.Marshal(answer)
	if err != nil {
		return false, fmt.Errorf("marshal answer: %w", err)
	}

	userQuery := fmt.Sprintf(`Verify this ARC puzzle answer:

## Puzzle (training examples + test input):
%s

## Proposed Answer:
%s

Does this answer correctly follow the transformation pattern from the training examples?`, string(puzzleJSON), string(answerJSON))

	stream := s.client.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
		Model: openai.ChatModel(s.model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(verifyPrompt),
			openai.UserMessage(userQuery),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
				JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:        "verify_response",
					Description: openai.String("Verification result"),
					Strict:      openai.Bool(true),
					Schema:      verifySchema,
				},
			},
		},
	})

	var contentBuilder strings.Builder
	for stream.Next() {
		chunk := stream.Current()
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			contentBuilder.WriteString(chunk.Choices[0].Delta.Content)
		}
	}

	if err := stream.Err(); err != nil {
		return false, fmt.Errorf("verify chat completion error: %w", err)
	}

	content := contentBuilder.String()
	if content == "" {
		return false, errors.New("no content in verify response")
	}

	var verifyResult VerifyResult
	if err := json.Unmarshal([]byte(content), &verifyResult); err != nil {
		start := strings.Index(content, "{")
		end := strings.LastIndex(content, "}")
		if start != -1 && end > start {
			if err := json.Unmarshal([]byte(content[start:end+1]), &verifyResult); err != nil {
				return false, fmt.Errorf("parse verify response: %w", err)
			}
		} else {
			return false, fmt.Errorf("invalid verify response format")
		}
	}

	if verifyResult.Reasoning != "" {
		fmt.Printf("%s🔍 Verification: %s%s\n", colorYellow, verifyResult.Reasoning, colorReset)
	}

	return verifyResult.Valid, nil
}
