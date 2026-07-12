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

func TestNewOpenClawFactory_EmptyGatewayURL_ReturnsStub(t *testing.T) {
	factory := NewOpenClawFactory(OpenClawConfig{})
	client, err := factory(ManagedBinding{Agent: "openclaw"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := client.(ManagedClient); !ok {
		t.Fatalf("expected ManagedClient stub, got %T", client)
	}
}

func TestNewOpenClawFactory_ReturnsOpenClawClient(t *testing.T) {
	factory := NewOpenClawFactory(OpenClawConfig{
		GatewayURL: "http://localhost:17772",
		Token:      "tok",
	})
	client, err := factory(ManagedBinding{Agent: "openclaw"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	oc, ok := client.(*OpenClawClient)
	if !ok {
		t.Fatalf("expected *OpenClawClient, got %T", client)
	}
	if oc.Agent != "openclaw/default" {
		t.Errorf("agent=%q want openclaw/default", oc.Agent)
	}
	if oc.GatewayURL != "http://localhost:17772" {
		t.Errorf("GatewayURL=%q", oc.GatewayURL)
	}
	if oc.Token != "tok" {
		t.Errorf("Token=%q", oc.Token)
	}
}

func TestNewOpenClawFactory_NonOpenclawAgent_PassThrough(t *testing.T) {
	factory := NewOpenClawFactory(OpenClawConfig{
		GatewayURL: "http://localhost:17772",
		Token:      "tok",
	})
	for _, tc := range []struct{ binding, wantModel string }{
		{"hermes", "openclaw/hermes"},
		{"claude-acp", "openclaw/claude-acp"},
		{"coding", "openclaw/coding"},
	} {
		client, err := factory(ManagedBinding{Agent: tc.binding})
		if err != nil {
			t.Fatalf("agent=%q: %v", tc.binding, err)
		}
		oc := client.(*OpenClawClient)
		if oc.Agent != tc.wantModel {
			t.Errorf("agent=%q: model=%q want %q", tc.binding, oc.Agent, tc.wantModel)
		}
	}
}

func TestRegistry_WithOpenClawFactory(t *testing.T) {
	factory := NewOpenClawFactory(OpenClawConfig{
		GatewayURL: "http://localhost:17772",
		Token:      "tok",
	})
	r := NewRegistry(RegistryConfig{
		Default:        &stubClient{name: "default"},
		ManagedFactory: factory,
	})
	client, err := r.ClientFor(store.AgentConfig{
		Harness:        "managed",
		RuntimeBinding: json.RawMessage(`{"agent":"openclaw"}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	oc, ok := client.(*OpenClawClient)
	if !ok {
		t.Fatalf("expected *OpenClawClient, got %T", client)
	}
	if oc.Agent != "openclaw/default" {
		t.Errorf("agent=%q want openclaw/default", oc.Agent)
	}
}

func TestNewHermesFactory_EmptyGatewayURL_ReturnsStub(t *testing.T) {
	factory := NewHermesFactory(HermesConfig{})
	client, err := factory(ManagedBinding{Agent: "hermes"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := client.(ManagedClient); !ok {
		t.Fatalf("expected ManagedClient stub, got %T", client)
	}
}

func TestNewHermesFactory_ReturnsHermesClient(t *testing.T) {
	factory := NewHermesFactory(HermesConfig{
		GatewayURL: "http://localhost:8642",
		Token:      "hermes-key",
	})
	client, err := factory(ManagedBinding{Agent: "hermes"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hc, ok := client.(*HermesClient)
	if !ok {
		t.Fatalf("expected *HermesClient, got %T", client)
	}
	if hc.Model != "hermes-agent" {
		t.Errorf("model=%q want hermes-agent", hc.Model)
	}
	if hc.GatewayURL != "http://localhost:8642" {
		t.Errorf("GatewayURL=%q", hc.GatewayURL)
	}
	if hc.Token != "hermes-key" {
		t.Errorf("Token=%q", hc.Token)
	}
}

func TestNewManagedFactory_DispatchesByAgent(t *testing.T) {
	factory := NewManagedFactory(
		OpenClawConfig{GatewayURL: "http://oc:17772", Token: "oc-tok"},
		HermesConfig{GatewayURL: "http://hc:8642", Token: "hc-tok"},
	)
	// hermes → HermesClient
	c, err := factory(ManagedBinding{Agent: "hermes"})
	if err != nil {
		t.Fatalf("hermes: %v", err)
	}
	if _, ok := c.(*HermesClient); !ok {
		t.Errorf("hermes: expected *HermesClient, got %T", c)
	}
	// openclaw → OpenClawClient
	c, err = factory(ManagedBinding{Agent: "openclaw"})
	if err != nil {
		t.Fatalf("openclaw: %v", err)
	}
	if _, ok := c.(*OpenClawClient); !ok {
		t.Errorf("openclaw: expected *OpenClawClient, got %T", c)
	}
	// unknown → defaults to OpenClawClient (OpenClaw pass-through)
	c, err = factory(ManagedBinding{Agent: "claude-acp"})
	if err != nil {
		t.Fatalf("claude-acp: %v", err)
	}
	if _, ok := c.(*OpenClawClient); !ok {
		t.Errorf("claude-acp: expected *OpenClawClient (pass-through), got %T", c)
	}
}

func TestNewManagedFactory_BothEmpty_ReturnsStub(t *testing.T) {
	factory := NewManagedFactory(OpenClawConfig{}, HermesConfig{})
	for _, agent := range []string{"hermes", "openclaw"} {
		c, err := factory(ManagedBinding{Agent: agent})
		if err != nil {
			t.Fatalf("%s: %v", agent, err)
		}
		if _, ok := c.(ManagedClient); !ok {
			t.Errorf("%s: expected ManagedClient stub, got %T", agent, c)
		}
	}
}
