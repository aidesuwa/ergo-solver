# ergo-solver

A Go CLI tool for automatically solving ARC (Abstraction and Reasoning Corpus) puzzles using AI.

## Features

- **Auto Login Check**: Prompts for cookie update when expired
- **Auto PoW Refresh**: Automatically handles Proof-of-Work challenges
- **AI Solving**: Uses LLM (OpenAI-compatible API) to solve ARC puzzles
- **AI Self-Verification**: Validates AI-generated answers before submission
- **Structured Output**: Uses JSON Schema to ensure correct AI output format

## Quick Start

```bash
# Clone the project
git clone https://github.com/aidesuwa/ergo-solver.git
cd ergo-solver

# Copy config file
cp config.example.json config.json
# Edit config.json with your cookie and API key

# Run
go run . solve --config config.json

# Or build and run
go build -o ergo-solver .
./ergo-solver solve --config config.json --count 3
```

## Configuration

```json
{
  "base_url": "https://your-target-site.example.com",
  "cookie": "cf_clearance=...; arc_session=...",
  "user_agent": "Mozilla/5.0 ...",
  "ai": {
    "enabled": true,
    "model": "claude-sonnet-4-5-20250929",
    "base_url": "https://api.openai.com/v1",
    "api_key": "sk-..."
  }
}
```

### Getting Cookie

1. Login to the target website
2. Open browser DevTools (F12) → Network
3. Refresh the page, copy the `Cookie` value from request headers

### AI Configuration

Supports any OpenAI-compatible API endpoint:

| Provider | base_url | Model Example |
|----------|----------|---------------|
| OpenAI | `https://api.openai.com/v1` | `gpt-4o` |
| Anthropic (via proxy) | Custom | `claude-sonnet-4-5-20250929` |
| Other compatible services | Custom | Per provider docs |

## Commands

```bash
# Solve puzzles (config required)
ergo-solver solve --config config.json

# Solve multiple puzzles
ergo-solver solve --config config.json --count 5

# Dry run (don't submit)
ergo-solver solve --config config.json --dry-run

# Auto mode (loop until daily limit)
ergo-solver solve --config config.json --auto

# Show help
ergo-solver help
```

## Options

| Option | Description |
|--------|-------------|
| `--config` | Path to config.json (required) |
| `--count` | Number of puzzles to solve (default: 1) |
| `--dry-run` | Solve but do not submit |
| `--auto` | Auto-loop until daily limit exhausted |

## Environment Variables

| Variable | Description |
|----------|-------------|
| `OPENAI_API_KEY` | OpenAI API Key (config file takes priority) |
| `NO_COLOR` | Disable colored output when set |

## Workflow

```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│ Check Login │ -> │ Refresh PoW │ -> │ Get Puzzle  │
└─────────────┘    └─────────────┘    └─────────────┘
                                            │
                                            v
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│   Submit    │ <- │ AI Verify   │ <- │  AI Solve   │
└─────────────┘    └─────────────┘    └─────────────┘
```

## Example Output

<details>
<summary>📋 Click to see example run output</summary>

```
go run . solve --config config.json --auto
2026-01-05T22:40:02-05:00 INF starting: count=1 dryRun=false autoLoop=true
2026-01-05T22:40:08-05:00 INF config.json updated (cookie refreshed)
2026-01-05T22:40:08-05:00 INF logged in: User(xxxxx)
2026-01-05T22:40:08-05:00 INF site: https://target-site.example.com
2026-01-05T22:40:09-05:00 INF daily quota: remaining=3 completed=2 limit=5
2026-01-05T22:40:09-05:00 INF PoW needs refresh, solving...
2026-01-05T22:40:09-05:00 INF PoW found nonce=17350 (elapsed 10ms)
2026-01-05T22:40:10-05:00 INF PoW verified
2026-01-05T22:40:10-05:00 INF AI using custom endpoint: https://your-api-endpoint.com/v1
2026-01-05T22:40:10-05:00 INF fetching puzzle: index=1/1
2026-01-05T22:40:10-05:00 INF puzzle fetched: puzzleId=f0df648a5ebc1af83c89278029df14d2, remainingAttempts=2, dailyRemaining=3/5

┌─────────────────────────────────────────┐
│      🤖 AI Agent Starting                │
│      📦 Model: gpt-5.2-codex           │
└─────────────────────────────────────────┘

💭 AI Reasoning:
──────────────────────────────────────────────────
Step 1: Identify two 4x5 blocks separated by a zero row; colors are {2,3} in top, {5,3} in bottom. Step 2: Compare all training pairs and note output is 4x5 with 3s only where both corresponding cells in top and bottom are 3; all other positions become 7. Step 3: Apply rule to test input by intersecting positions of value 3 in both blocks, fill those with 3 and the rest with 7.
──────────────────────────────────────────────────
📊 Confidence: 86%
⠼ 🔄 AI self-verifying... [30s]  🔍 Verification: From training, output is 3 only where the corresponding cells in the top 4x5 block and bottom 4x5 block are both 3; otherwise 7. Applying this to the test input yields [[7,3,7,7,7],[7,7,3,7,7],[7,7,3,3,3],[7,7,7,7,3]], which matches the proposed answer.
✅ AI self-verification passed!
✨ Answer generated!
2026-01-05T22:41:29-05:00 INF AI solved (elapsed 1m12.08s)
2026-01-05T22:41:30-05:00 INF PoW valid, no refresh needed
2026-01-05T22:41:30-05:00 INF config.json updated (cookie refreshed)
2026-01-05T22:41:30-05:00 INF submitting: puzzleId=f0df648a5ebc1af83c89278029df14d2
2026-01-05T22:41:30-05:00 INF submit response: 恭喜！你成功解开了谜题！
2026-01-05T22:41:30-05:00 INF correct: +10 points, balance=60, dailyRemaining=2/5
2026-01-05T22:41:30-05:00 INF auto mode: sleeping 1m37s (remaining 2)...
2026-01-05T22:43:18-05:00 INF fetching puzzle: index=2/2
2026-01-05T22:43:19-05:00 INF config.json updated (cookie refreshed)
2026-01-05T22:43:19-05:00 INF puzzle fetched: puzzleId=d3fc76f87e23f6ce945bb01adad8d3df, remainingAttempts=2, dailyRemaining=2/5
```

</details>

## Development

```bash
# Run tests
go test ./...

# Format code
go fmt ./...

# Build
go build -o ergo-solver .
```

## License

MIT License - See [LICENSE](LICENSE)
