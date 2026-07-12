#!/usr/bin/env python3
"""
Test script to demonstrate different harness types support in agent creation.

This script shows how to create agents with different harness backends:
- pipy (default): Platform's Python sidecar
- hermes: Hermes agent via Runs API
- openclaw: OpenClaw Gateway
- default-loop: Alias for pipy
- managed: Managed harness (stub)
"""
from __future__ import annotations

import os
import sys

# Add SDK to path
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from oma_sdk import OMAClient
from oma_sdk.api.agents import _build_metadata, MODEL


def test_harness_types():
    """Test creating agents with different harness types."""

    # Set environment variables
    os.environ.setdefault("OMA_API_KEY", "dev-key")
    base_url = os.getenv("OMA_BASE_URL", "http://127.0.0.1:8787")

    # Initialize client
    client = OMAClient(base_url=base_url)

    print("="*80)
    print("Testing Agent Creation with Different Harness Types")
    print("="*80)

    harness_tests = [
        ("pipy", "default-loop"),
        ("hermes", "hermes"),
        ("openclaw", "openclaw"),
        ("default-loop", "default-loop"),
        ("managed", "managed"),
    ]

    for harness_name, expected_value in harness_tests:
        print(f"\n[Testing] Creating agent with {harness_name} harness...")
        try:
            # Build metadata
            metadata = _build_metadata(harness_name)

            # Create agent
            create_kwargs = {
                "name": f"test-{harness_name}-harness",
                "model": MODEL
            }
            if metadata:
                create_kwargs["metadata"] = metadata

            agent = client.agents.create(**create_kwargs)

            print(f"✓ Success: Agent {agent.id} created with {harness_name} harness")
            if hasattr(agent, 'metadata') and agent.metadata:
                print(f"  Metadata: {agent.metadata}")
                actual_harness = agent.metadata.get('_oma.harness', 'N/A')
                print(f"  Harness value: {actual_harness}")
                if actual_harness == expected_value:
                    print(f"  ✓ Harness correctly set to '{expected_value}'")
                else:
                    print(f"  ✗ Expected '{expected_value}', got '{actual_harness}'")

            # Archive the agent
            try:
                client.agents.archive(agent.id)
                print(f"  ✓ Agent archived")
            except Exception as e:
                print(f"  ! Failed to archive agent: {e}")

        except Exception as e:
            print(f"✗ Failed: {e}")
            import traceback
            traceback.print_exc()

    print("\n" + "="*80)
    print("Test completed!")
    print("="*80)


if __name__ == "__main__":
    test_harness_types()
