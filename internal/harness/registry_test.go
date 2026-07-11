package harness

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/open-ma/oma-building/internal/store"
)

// stubClient is a minimal Client for registry tests.
type stubClient struct{ name string }

func (s *stubClient) RunTurn(context.Context, TurnRequest) (TurnResponse, error) {
	return TurnResponse{}, nil
}

func TestClientFor_DefaultLoop_WhenHarnessEmpty(t *testing.T) {
	def := &stubClient{name: "default"}
	r := NewRegistry(RegistryConfig{Default: def})
	got, err := r.ClientFor(store.AgentConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != def {
		t.Fatalf("expected default client, got %v", got)
	}
}

func TestClientFor_DefaultLoop_WhenHarnessExplicit(t *testing.T) {
	def := &stubClient{name: "default"}
	r := NewRegistry(RegistryConfig{Default: def})
	got, err := r.ClientFor(store.AgentConfig{Harness: "default-loop"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != def {
		t.Fatalf("expected default client")
	}
}

func TestClientFor_DefaultLoop_PipyAlias(t *testing.T) {
	def := &stubClient{name: "default"}
	r := NewRegistry(RegistryConfig{Default: def})
	got, err := r.ClientFor(store.AgentConfig{Harness: "pipy"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != def {
		t.Fatalf("pipy alias should resolve to default-loop client")
	}
}

func TestClientFor_Managed_CallsFactory(t *testing.T) {
	var captured ManagedBinding
	factory := func(b ManagedBinding) (Client, error) {
		captured = b
		return &stubClient{name: "managed"}, nil
	}
	r := NewRegistry(RegistryConfig{ManagedFactory: factory})
	raw := json.RawMessage(`{"agent":"hermes"}`)
	_, err := r.ClientFor(store.AgentConfig{Harness: "managed", RuntimeBinding: raw})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured.Agent != "hermes" {
		t.Fatalf("factory should see agent=hermes, got %q", captured.Agent)
	}
}

func TestClientFor_Managed_MissingBinding(t *testing.T) {
	factory := func(ManagedBinding) (Client, error) {
		return &stubClient{}, nil
	}
	r := NewRegistry(RegistryConfig{ManagedFactory: factory})
	_, err := r.ClientFor(store.AgentConfig{Harness: "managed"})
	if err == nil {
		t.Fatalf("expected error for missing runtime_binding")
	}
}

func TestClientFor_Managed_MissingAgentField(t *testing.T) {
	factory := func(ManagedBinding) (Client, error) {
		return &stubClient{}, nil
	}
	r := NewRegistry(RegistryConfig{ManagedFactory: factory})
	_, err := r.ClientFor(store.AgentConfig{
		Harness:        "managed",
		RuntimeBinding: json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatalf("expected error for missing agent field")
	}
}

func TestClientFor_Managed_FactoryNotConfigured(t *testing.T) {
	r := NewRegistry(RegistryConfig{})
	_, err := r.ClientFor(store.AgentConfig{
		Harness:        "managed",
		RuntimeBinding: json.RawMessage(`{"agent":"hermes"}`),
	})
	if err == nil {
		t.Fatalf("expected error when managed factory not configured")
	}
}

func TestClientFor_Fake_ReturnsFakeClient(t *testing.T) {
	custom := &stubClient{name: "custom-fake"}
	r := NewRegistry(RegistryConfig{Fake: custom})
	got, err := r.ClientFor(store.AgentConfig{Harness: "fake"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != custom {
		t.Fatalf("expected custom fake client")
	}
}

func TestClientFor_Fake_DefaultsToFakeClient(t *testing.T) {
	r := NewRegistry(RegistryConfig{})
	got, err := r.ClientFor(store.AgentConfig{Harness: "fake"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.(*FakeClient); !ok {
		t.Fatalf("expected *FakeClient when Fake not configured, got %T", got)
	}
}

func TestClientFor_Unknown(t *testing.T) {
	r := NewRegistry(RegistryConfig{Default: &stubClient{}})
	_, err := r.ClientFor(store.AgentConfig{Harness: "nonsense"})
	if err == nil {
		t.Fatalf("expected error for unknown kind")
	}
}

func TestClientFor_Force_OverridesEverything(t *testing.T) {
	def := &stubClient{name: "default"}
	force := &stubClient{name: "force"}
	r := NewRegistry(RegistryConfig{Default: def, Force: force})
	for _, kind := range []string{"", "default-loop", "pipy", "managed", "fake", "nonsense"} {
		got, err := r.ClientFor(store.AgentConfig{
			Harness:        kind,
			RuntimeBinding: json.RawMessage(`{"agent":"hermes"}`),
		})
		if err != nil {
			t.Fatalf("kind=%q: unexpected error: %v", kind, err)
		}
		if got != force {
			t.Fatalf("kind=%q: Force should override, got %v", kind, got)
		}
	}
}

func TestDefaultOnly_AlwaysReturnsDefault(t *testing.T) {
	def := &stubClient{name: "default"}
	r := DefaultOnly(def)
	// DefaultOnly covers the existing-test case where agents have no
	// _oma.harness set. It accepts "" / "default-loop" / "pipy" / "fake".
	// It does NOT silently swallow "managed" — that would mask test bugs.
	for _, kind := range []string{"", "default-loop", "pipy", "fake"} {
		got, err := r.ClientFor(store.AgentConfig{Harness: kind})
		if err != nil {
			t.Fatalf("kind=%q: unexpected error: %v", kind, err)
		}
		if got != def && kind != "fake" {
			t.Fatalf("kind=%q: DefaultOnly should return default for harness kinds used by existing tests", kind)
		}
	}
	// "managed" without a factory must still fail loudly.
	_, err := r.ClientFor(store.AgentConfig{
		Harness:        "managed",
		RuntimeBinding: json.RawMessage(`{"agent":"hermes"}`),
	})
	if err == nil {
		t.Fatalf("DefaultOnly should not silently serve managed kind")
	}
}

func TestParseManagedBinding(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		b, err := ParseManagedBinding(json.RawMessage(`{"agent":"openclaw"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if b.Agent != "openclaw" {
			t.Fatalf("expected agent=openclaw, got %q", b.Agent)
		}
	})
	t.Run("empty raw", func(t *testing.T) {
		_, err := ParseManagedBinding(nil)
		if err == nil {
			t.Fatalf("expected error for nil raw")
		}
	})
	t.Run("missing agent", func(t *testing.T) {
		_, err := ParseManagedBinding(json.RawMessage(`{}`))
		if err == nil {
			t.Fatalf("expected error for missing agent")
		}
	})
	t.Run("malformed json", func(t *testing.T) {
		_, err := ParseManagedBinding(json.RawMessage(`{not json`))
		if err == nil {
			t.Fatalf("expected error for malformed json")
		}
	})
}
