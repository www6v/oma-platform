package harness

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"time"
)

// usageEvent builds a span.model_request_end event compatible with the
// usage.AggregateEvents pipeline. The event carries the model id, token
// counts, provider tag, and duration so the tenant usage report can
// break numbers down by backend.
//
// Schema consumed by usage.ApplySpanUsage:
//
//	{
//	  "type": "span.model_request_end",
//	  "id":   "<span id>",
//	  "model": "<model id>",
//	  "provider": "<provider tag>",
//	  "duration_ms": <int>,
//	  "model_usage": {
//	    "input_tokens": <int>,
//	    "output_tokens": <int>,
//	    "cache_read_input_tokens": <int>,
//	    "cache_creation_input_tokens": <int>,
//	  }
//	}
func usageEvent(
	model, provider string,
	duration time.Duration,
	u *TurnUsage,
) (json.RawMessage, error) {
	usage := map[string]any{}
	if u != nil {
		usage["input_tokens"] = u.InputTokens
		usage["output_tokens"] = u.OutputTokens
		if u.CacheReadInputTokens > 0 {
			usage["cache_read_input_tokens"] = u.CacheReadInputTokens
		}
		if u.CacheCreationInputTokens > 0 {
			usage["cache_creation_input_tokens"] = u.CacheCreationInputTokens
		}
	}
	return json.Marshal(map[string]any{
		"type":        "span.model_request_end",
		"id":          randomSpanID(),
		"model":       model,
		"provider":    provider,
		"duration_ms": duration.Milliseconds(),
		"model_usage": usage,
	})
}

const spanIDAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

func randomSpanID() string {
	out := make([]byte, 12)
	max := big.NewInt(int64(len(spanIDAlphabet)))
	for i := range out {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			out[i] = spanIDAlphabet[i%len(spanIDAlphabet)]
			continue
		}
		out[i] = spanIDAlphabet[idx.Int64()]
	}
	return "span_" + string(out)
}

// logTurn emits a one-line structured log for a managed turn. The format
// is intentionally grep-friendly: key=value pairs, no structured JSON
// (the project uses stdlib log; JSON would be a larger change).
//
// Example:
//
//	managed.turn backend=openclaw session=sess-abc model=openclaw/default
//	  duration_ms=482 input_tokens=17 output_tokens=3 stream=false
func logTurn(args ...any) {
	if len(args)%2 != 0 {
		// Defensive — should never happen from our call sites.
		log.Print(append([]any{"managed.turn malformed kv:"}, args...)...)
		return
	}
	msg := "managed.turn"
	for i := 0; i < len(args); i += 2 {
		msg += fmt.Sprintf(" %v=%v", args[i], args[i+1])
	}
	log.Print(msg)
}

// valueOrZero extracts a value from a possibly-nil pointer via the
// given accessor, returning zero when the pointer is nil. Keeps log
// call sites tidy.
func valueOrZero[T any, R any](p *T, f func(*T) R) R {
	var zero R
	if p == nil {
		return zero
	}
	return f(p)
}
