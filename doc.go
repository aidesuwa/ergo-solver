// Package main implements ergo-solver, a CLI tool for automatically solving
// ARC (Abstraction and Reasoning Corpus) puzzles using AI.
//
// # Features
//
//   - Automatic session management with cookie persistence
//   - Proof-of-Work (PoW) challenge solving
//   - AI-powered puzzle solving via OpenAI-compatible APIs
//   - Self-verification of AI-generated answers
//   - Structured output with JSON Schema validation
//
// # Usage
//
//	ergo-solver solve --config PATH [--count N] [--dry-run] [--auto]
//
// # Configuration
//
// Configuration is loaded from config.json in the current directory or the
// path specified by ERGO_PROXY_HOME environment variable.
//
// See README.md for detailed configuration options.
package main
