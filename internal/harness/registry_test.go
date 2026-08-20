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
	// Legacy managed+hermes dispatches to HermesClient via normalizeKind.
	hc := &stubClient{name: "hermes"}
	r := NewRegistry(RegistryConfig{Default: &stubClient{}, Hermes: hc})
	raw := json.RawMessage(`{"agent":"hermes"}`)
	got, err := r.ClientFor(store.AgentConfig{Harness: "managed", RuntimeBinding: raw})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != hc {
		t.Fatalf("expected hermes client for managed+hermes")
	}
}

func TestClientFor_Managed_MissingBinding(t *testing.T) {
	r := NewRegistry(RegistryConfig{Default: &stubClient{}, Hermes: &stubClient{}})
	_, err := r.ClientFor(store.AgentConfig{Harness: "managed"})
	if err == nil {
		t.Fatalf("expected error for missing runtime_binding")
	}
}

func TestClientFor_Managed_MissingAgentField(t *testing.T) {
	r := NewRegistry(RegistryConfig{Default: &stubClient{}, OpenClaw: &stubClient{}})
	_, err := r.ClientFor(store.AgentConfig{
		Harness:        "managed",
		RuntimeBinding: json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatalf("expected error for missing agent field")
	}
}

func TestClientFor_Managed_FactoryNotConfigured_ReturnsStub(t *testing.T) {
	r := NewRegistry(RegistryConfig{})
	got, err := r.ClientFor(store.AgentConfig{
		Harness:        "managed",
		RuntimeBinding: json.RawMessage(`{"agent":"hermes"}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.(ManagedClient); !ok {
		t.Fatalf("expected ManagedClient stub, got %T", got)
	}
}

func TestManagedClient_RunTurn_ReturnsNotImplemented(t *testing.T) {
	c := ManagedClient{}
	_, err := c.RunTurn(context.Background(), TurnRequest{})
	if err == nil {
		t.Fatalf("expected error from ManagedClient.RunTurn")
	}
	if got := err.Error(); got != managedNotImplemented {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestIsKnownAgent(t *testing.T) {
	for _, a := range KnownAgents {
		if !IsKnownAgent(a) {
			t.Errorf("expected %q to be known", a)
		}
	}
	for _, a := range []string{"", "unknown", "HERMES", "hermes "} {
		if IsKnownAgent(a) {
			t.Errorf("expected %q to be unknown", a)
		}
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
	for _, kind := range []string{"", "default-loop", "pipy", "managed", "hermes", "openclaw", "fake", "nonsense"} {
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
	for _, kind := range []string{"", "default-loop", "pipy", "fake"} {
		got, err := r.ClientFor(store.AgentConfig{Harness: kind})
		if err != nil {
			t.Fatalf("kind=%q: unexpected error: %v", kind, err)
		}
		if got != def && kind != "fake" {
			t.Fatalf("kind=%q: DefaultOnly should return default for harness kinds used by existing tests", kind)
		}
	}
	// Phase 3: managed kind falls back to the ManagedClient stub so tests
	// that happen to touch managed agents don't crash the registry.
	got, err := r.ClientFor(store.AgentConfig{
		Harness:        "managed",
		RuntimeBinding: json.RawMessage(`{"agent":"hermes"}`),
	})
	if err != nil {
		t.Fatalf("managed kind should resolve to stub: %v", err)
	}
	if _, ok := got.(ManagedClient); !ok {
		t.Fatalf("expected ManagedClient stub, got %T", got)
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

func TestClientFor_DeepSeekKind(t *testing.T) {
	ds := &stubClient{name: "deepseek"}
	r := NewRegistry(RegistryConfig{Default: &stubClient{}, DeepSeek: ds})
	got, err := r.ClientFor(store.AgentConfig{Harness: "deepseek"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != ds {
		t.Fatalf("expected deepseek client")
	}
}

func TestClientFor_DeepSeekKind_NotConfigured_ReturnsStub(t *testing.T) {
	r := NewRegistry(RegistryConfig{Default: &stubClient{}})
	got, err := r.ClientFor(store.AgentConfig{Harness: "deepseek"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.(ManagedClient); !ok {
		t.Fatalf("expected ManagedClient stub, got %T", got)
	}
}

func TestClientFor_HermesKind_Flat(t *testing.T) {
	hc := &stubClient{name: "hermes"}
	r := NewRegistry(RegistryConfig{Default: &stubClient{}, Hermes: hc})
	got, err := r.ClientFor(store.AgentConfig{Harness: "hermes"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != hc {
		t.Fatalf("expected hermes client")
	}
}

func TestClientFor_OpenClawKind_Flat(t *testing.T) {
	oc := &stubClient{name: "openclaw"}
	r := NewRegistry(RegistryConfig{Default: &stubClient{}, OpenClaw: oc})
	got, err := r.ClientFor(store.AgentConfig{Harness: "openclaw"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != oc {
		t.Fatalf("expected openclaw client")
	}
}

func TestClientFor_LegacyManaged_Hermes(t *testing.T) {
	hc := &stubClient{name: "hermes"}
	r := NewRegistry(RegistryConfig{Default: &stubClient{}, Hermes: hc})
	got, err := r.ClientFor(store.AgentConfig{
		Harness:        "managed",
		RuntimeBinding: json.RawMessage(`{"agent":"hermes"}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != hc {
		t.Fatalf("managed+hermes must normalize to hermes client")
	}
}

func TestClientFor_LegacyManaged_OpenClaw(t *testing.T) {
	oc := &stubClient{name: "openclaw"}
	r := NewRegistry(RegistryConfig{Default: &stubClient{}, OpenClaw: oc})
	got, err := r.ClientFor(store.AgentConfig{
		Harness:        "managed",
		RuntimeBinding: json.RawMessage(`{"agent":"openclaw"}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != oc {
		t.Fatalf("managed+openclaw must normalize to openclaw client")
	}
}

func TestClientFor_HermesKind_NotConfigured_ReturnsStub(t *testing.T) {
	r := NewRegistry(RegistryConfig{Default: &stubClient{}})
	got, err := r.ClientFor(store.AgentConfig{Harness: "hermes"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.(ManagedClient); !ok {
		t.Fatalf("expected ManagedClient stub, got %T", got)
	}
}

func TestNormalizeKind(t *testing.T) {
	cases := []struct {
		harness string
		binding json.RawMessage
		want    Kind
	}{
		{"", nil, KindDefaultLoop},
		{"pipy", nil, KindDefaultLoop},
		{"default-loop", nil, KindDefaultLoop},
		{"hermes", nil, KindHermes},
		{"openclaw", nil, KindOpenClaw},
		{"deepseek", nil, KindDeepSeek},
		{"fake", nil, KindFake},
		{"managed", json.RawMessage(`{"agent":"hermes"}`), KindHermes},
		{"managed", json.RawMessage(`{"agent":"openclaw"}`), KindOpenClaw},
		{"managed", json.RawMessage(`{"agent":"claude-acp"}`), KindOpenClaw},
	}
	for _, c := range cases {
		got, err := normalizeKind(
			store.AgentConfig{Harness: c.harness, RuntimeBinding: c.binding},
		)
		if err != nil {
			t.Fatalf("%q: unexpected error: %v", c.harness, err)
		}
		if got != c.want {
			t.Errorf("%q+%s: got %q want %q",
				c.harness, string(c.binding), got, c.want)
		}
	}
}

func TestNormalizeKind_Managed_Errors(t *testing.T) {
	cases := []struct {
		name    string
		binding json.RawMessage
	}{
		{"missing binding", nil},
		{"empty object", json.RawMessage(`{}`)},
		{"malformed json", json.RawMessage(`{not json`)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := normalizeKind(store.AgentConfig{
				Harness:        "managed",
				RuntimeBinding: c.binding,
			})
			if err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestOpenclawModel(t *testing.T) {
	cases := []struct{ binding, want string }{
		{"openclaw", "openclaw/default"},
		{"hermes", "openclaw/hermes"},
		{"claude-acp", "openclaw/claude-acp"},
		{"coding", "openclaw/coding"},
	}
	for _, c := range cases {
		if got := OpenclawModel(c.binding); got != c.want {
			t.Errorf("OpenclawModel(%q) = %q, want %q",
				c.binding, got, c.want)
		}
	}
}
