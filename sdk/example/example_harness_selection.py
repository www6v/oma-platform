#!/usr/bin/env python3
"""
Example: Create agents with different harness types.

This example demonstrates how to use the harness parameter when creating agents.
It shows three different harness backends: pipy, hermes, and openclaw.

Usage:
    OMA_API_KEY=dev-key OMA_BASE_URL=http://127.0.0.1:8787 \
        python example_harness_selection.py
"""
from __future__ import annotations

import os
import sys

# Ensure Python 3.11+
if sys.version_info < (3, 11):
    sys.exit("Python 3.11+ required")

from oma_sdk import OMAClient
from oma_sdk.api.agents import _build_metadata, MODEL


def create_agent_with_harness(client: OMAClient, harness_type: str, name: str) -> dict:
    """Helper to create an agent with specified harness."""
    metadata = _build_metadata(harness_type)
    create_kwargs = {
        "name": name,
        "model": MODEL
    }
    if metadata:
        create_kwargs["metadata"] = metadata

    agent = client.agents.create(**create_kwargs)

    print(f"✓ Agent created: {agent.id}")
    print(f"  Name: {agent.name}")
    print(f"  Model: {agent.model}")
    if hasattr(agent, "metadata") and agent.metadata:
        print(f"  Metadata: {agent.metadata}")
        print(f"  Harness: {agent.metadata.get('_oma.harness', 'N/A')}")

    return {"agent": agent}


def create_agent_with_pipy(client: OMAClient) -> dict:
    """Create an agent using the default pipy harness."""
    print("\n" + "="*60)
    print("Creating agent with PI PY harness (default)")
    print("="*60)

    return create_agent_with_harness(client, "pipy", "pipy-agent-demo")


def create_agent_with_hermes(client: OMAClient) -> dict:
    """Create an agent using the hermes harness."""
    print("\n" + "="*60)
    print("Creating agent with HERMES harness")
    print("="*60)

    return create_agent_with_harness(client, "hermes", "hermes-agent-demo")


def create_agent_with_openclaw(client: OMAClient) -> dict:
    """Create an agent using the openclaw harness."""
    print("\n" + "="*60)
    print("Creating agent with OPENCLAW harness")
    print("="*60)

    return create_agent_with_harness(client, "openclaw", "openclaw-agent-demo")


def main():
    """Main entry point."""
    # Set environment variables
    os.environ.setdefault("OMA_API_KEY", "dev-key")
    base_url = os.getenv("OMA_BASE_URL", "http://127.0.0.1:8787")

    print(f"OMA Base URL: {base_url}")
    print(f"API Key: {'*' * 8}")

    # Initialize client
    client = OMAClient(base_url=base_url)

    results = {}

    # Create agents with different harnesses
    try:
        results["pipy"] = create_agent_with_pipy(client)
    except Exception as e:
        print(f"✗ Failed to create pipy agent: {e}")

    try:
        results["hermes"] = create_agent_with_hermes(client)
    except Exception as e:
        print(f"✗ Failed to create hermes agent: {e}")

    try:
        results["openclaw"] = create_agent_with_openclaw(client)
    except Exception as e:
        print(f"✗ Failed to create openclaw agent: {e}")

    # Summary
    print("\n" + "="*60)
    print("SUMMARY")
    print("="*60)

    for harness_type, result in results.items():
        if result:
            agent = result["agent"]
            print(f"\n{harness_type.upper()} Agent:")
            print(f"  ID: {agent.id}")
            print(f"  Name: {agent.name}")
            if hasattr(agent, "metadata") and agent.metadata:
                print(f"  Harness: {agent.metadata.get('_oma.harness', 'default-loop')}")

    print("\n" + "="*60)
    print(f"Successfully created {len(results)} agents with different harnesses")
    print("="*60)


if __name__ == "__main__":
    main()
