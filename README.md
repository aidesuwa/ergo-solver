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
