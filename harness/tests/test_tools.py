from oma_adapter.tools import pypi_tools_from_agent
from oma_adapter.types import AgentSnapshot


def test_default_tools_when_missing() -> None:
    agent = AgentSnapshot(id="a", name="n", model="m")
    assert pypi_tools_from_agent(agent) == ["read", "bash", "write"]


def test_agent_toolset_maps_to_pipy_tools() -> None:
    agent = AgentSnapshot(
        id="a",
        name="n",
        model="m",
        tools=[{"type": "agent_toolset_20260401"}],
    )
    assert pypi_tools_from_agent(agent) == ["read", "bash", "write"]
