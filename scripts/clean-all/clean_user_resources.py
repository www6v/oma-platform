#!/usr/bin/env python3
"""
Script to delete all resources for a specific user in the OMA system.

This script uses the OMA SDK to delete all resources associated with a user,
including agents, sessions, environments, vaults, memory stores, skills,
and uploaded files (GET/DELETE /v1/files).
"""

import argparse
import os
import sys
from pathlib import Path
from typing import Iterator, Optional

import anthropic
import httpx

# Allow importing oma_sdk from meta-harness/sdk without a pip install.
_SDK_DIR = Path(__file__).resolve().parents[2] / "sdk"
if str(_SDK_DIR) not in sys.path:
    sys.path.insert(0, str(_SDK_DIR))

from oma_sdk.examples import AgentExamples

_DEFAULT_BASE_URL = "http://127.0.0.1:8787"
_DEFAULT_API_KEY = "dev-key"


def _load_dotenv() -> None:
    """Load meta-harness/.env when present."""
    dotenv = Path(__file__).resolve().parents[2] / ".env"
    if not dotenv.exists():
        return
    for line in dotenv.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        os.environ.setdefault(key.strip(), value.strip())


def get_client(base_url: Optional[str] = None) -> anthropic.Anthropic:
    """Initialize and return the OMA API client (anthropic SDK + custom base URL)."""
    api_key = os.getenv("OMA_API_KEY", _DEFAULT_API_KEY)
    resolved_base_url = base_url or os.getenv("OMA_BASE_URL", _DEFAULT_BASE_URL)
    return anthropic.Anthropic(api_key=api_key, base_url=resolved_base_url)


def delete_all_sessions(client: anthropic.Anthropic, user_id: Optional[str] = None, dry_run: bool = False) -> int:
    """Delete all sessions, optionally filtered by user_id."""
    count = 0
    try:
        page = client.beta.sessions.list()
        sessions = list(page)
        
        for session in sessions:
            # Filter by user_id if provided (assuming session has user metadata)
            if user_id and hasattr(session, 'user_id') and session.user_id != user_id:
                continue
                
            print(f"{'[DRY RUN] ' if dry_run else ''}Deleting session {session.id}")
            if not dry_run:
                try:
                    client.beta.sessions.archive(session.id)
                    client.beta.sessions.delete(session.id)
                except Exception as e:
                    print(f"  Error deleting session {session.id}: {e}")
                    continue
            count += 1
            
    except Exception as e:
        print(f"Error listing/deleting sessions: {e}")
    
    return count


def delete_all_agents(client: anthropic.Anthropic, user_id: Optional[str] = None, dry_run: bool = False) -> int:
    """Archive and delete all agents, optionally filtered by user_id."""
    count = 0
    try:
        # Default list() only returns active agents; include archived so we
        # can permanently delete agents left from prior archive-only runs.
        agents = AgentExamples.list_all_agents(client, include_archived=True)
        print(f"Found {len(agents)} agents (including archived)")

        for agent in agents:
            # Filter by user_id if provided (assuming agent has user metadata)
            if user_id and hasattr(agent, 'user_id') and agent.user_id != user_id:
                continue

            label = f"{agent.id} ({agent.name})"
            if getattr(agent, "archived_at", None):
                label += " [archived]"
            print(f"{'[DRY RUN] ' if dry_run else ''}Deleting agent {label}")
            if not dry_run:
                try:
                    AgentExamples.cleanup_agent(client, agent.id)
                except Exception as e:
                    print(f"  Error deleting agent {agent.id}: {e}")
                    continue
            count += 1

    except Exception as e:
        print(f"Error listing/deleting agents: {e}")

    return count


def delete_all_environments(client: anthropic.Anthropic, user_id: Optional[str] = None, dry_run: bool = False) -> int:
    """Archive and delete all environments, optionally filtered by user_id."""
    count = 0
    try:
        page = client.beta.environments.list()
        environments = list(page)
        
        for env in environments:
            # Filter by user_id if provided (assuming environment has user metadata)
            if user_id and hasattr(env, 'user_id') and env.user_id != user_id:
                continue
                
            print(f"{'[DRY RUN] ' if dry_run else ''}Archiving and deleting environment {env.id} ({env.name})")
            if not dry_run:
                try:
                    # First archive the environment
                    client.beta.environments.archive(env.id)
                    # Then delete it permanently
                    try:
                        client.beta.environments.delete(env.id)
                    except AttributeError:
                        # If delete method doesn't exist, archive is sufficient
                        print(f"  Note: Delete method not available, environment archived only")
                except Exception as e:
                    print(f"  Error deleting environment {env.id}: {e}")
                    continue
            count += 1
            
    except Exception as e:
        print(f"Error listing/deleting environments: {e}")
    
    return count


def delete_all_vaults(client: anthropic.Anthropic, user_id: Optional[str] = None, dry_run: bool = False) -> int:
    """Archive and delete all vaults, optionally filtered by user_id."""
    count = 0
    try:
        page = client.beta.vaults.list()
        vaults = list(page)
        
        for vault in vaults:
            # Filter by user_id if provided (assuming vault has user metadata)
            if user_id and hasattr(vault, 'user_id') and vault.user_id != user_id:
                continue
                
            print(f"{'[DRY RUN] ' if dry_run else ''}Archiving and deleting vault {vault.id}")
            if not dry_run:
                try:
                    # First archive the vault
                    client.beta.vaults.archive(vault.id)
                    # Then delete it permanently
                    try:
                        client.beta.vaults.delete(vault.id)
                    except AttributeError:
                        # If delete method doesn't exist, archive is sufficient
                        print(f"  Note: Delete method not available, vault archived only")
                except Exception as e:
                    print(f"  Error deleting vault {vault.id}: {e}")
                    continue
            count += 1
            
    except Exception as e:
        print(f"Error listing/deleting vaults: {e}")
    
    return count


def delete_all_memory_stores(client: anthropic.Anthropic, user_id: Optional[str] = None, dry_run: bool = False) -> int:
    """Archive and delete all memory stores, optionally filtered by user_id."""
    count = 0
    try:
        page = client.beta.memory_stores.list()
        memory_stores = list(page)
        
        for ms in memory_stores:
            # Filter by user_id if provided (assuming memory store has user metadata)
            if user_id and hasattr(ms, 'user_id') and ms.user_id != user_id:
                continue
                
            print(f"{'[DRY RUN] ' if dry_run else ''}Archiving and deleting memory store {ms.id} ({ms.name})")
            if not dry_run:
                try:
                    # First archive the memory store
                    client.beta.memory_stores.archive(ms.id)
                    # Then delete it permanently
                    try:
                        client.beta.memory_stores.delete(ms.id)
                    except AttributeError:
                        # If delete method doesn't exist, archive is sufficient
                        print(f"  Note: Delete method not available, memory store archived only")
                except Exception as e:
                    print(f"  Error deleting memory store {ms.id}: {e}")
                    continue
            count += 1
            
    except Exception as e:
        print(f"Error listing/deleting memory stores: {e}")
    
    return count


def delete_all_skills(client: anthropic.Anthropic, user_id: Optional[str] = None, dry_run: bool = False) -> int:
    """Delete all custom skills, optionally filtered by user_id."""
    count = 0
    try:
        page = client.beta.skills.list()
        skills = list(page)
        
        for skill in skills:
            # Skip built-in skills (they typically have specific IDs or metadata)
            if hasattr(skill, 'builtin') and skill.builtin:
                continue
            if skill.id.startswith('builtin_'):
                continue
                
            # Filter by user_id if provided (assuming skill has user metadata)
            if user_id and hasattr(skill, 'user_id') and skill.user_id != user_id:
                continue
                
            print(f"{'[DRY RUN] ' if dry_run else ''}Deleting skill {skill.id}")
            if not dry_run:
                try:
                    client.beta.skills.delete(skill.id)
                except Exception as e:
                    print(f"  Error deleting skill {skill.id}: {e}")
                    continue
            count += 1
            
    except Exception as e:
        print(f"Error listing/deleting skills: {e}")
    
    return count


def _iter_deletable_files(http: httpx.Client) -> Iterator[dict]:
    """Yield all deletable file records from GET /v1/files (paginated)."""
    before_id: Optional[str] = None
    while True:
        params: dict = {"limit": 1000, "order": "desc"}
        if before_id:
            params["before_id"] = before_id
        resp = http.get("/v1/files", params=params)
        resp.raise_for_status()
        payload = resp.json()
        for row in payload.get("data") or []:
            file_id = row.get("id") or ""
            if file_id and not file_id.startswith("out:"):
                yield row
        if not payload.get("has_more"):
            break
        before_id = payload.get("last_id")
        if not before_id:
            break


def delete_all_files(
    base_url: str,
    dry_run: bool = False,
) -> int:
    """Delete all uploaded files via GET/DELETE /v1/files."""
    api_key = os.getenv("OMA_API_KEY", _DEFAULT_API_KEY)
    count = 0
    try:
        with httpx.Client(
            base_url=base_url,
            headers={"x-api-key": api_key},
            timeout=30.0,
        ) as http:
            if dry_run:
                for row in _iter_deletable_files(http):
                    file_id = row["id"]
                    filename = row.get("filename", file_id)
                    print(f"[DRY RUN] Deleting file {file_id} ({filename})")
                    count += 1
                return count

            while True:
                resp = http.get("/v1/files", params={"limit": 1000})
                resp.raise_for_status()
                rows = [
                    row for row in (resp.json().get("data") or [])
                    if (row.get("id") or "")
                    and not (row.get("id") or "").startswith("out:")
                ]
                if not rows:
                    break
                for row in rows:
                    file_id = row["id"]
                    filename = row.get("filename", file_id)
                    print(f"Deleting file {file_id} ({filename})")
                    try:
                        del_resp = http.delete(f"/v1/files/{file_id}")
                        del_resp.raise_for_status()
                    except Exception as e:
                        print(f"  Error deleting file {file_id}: {e}")
                        continue
                    count += 1
    except Exception as e:
        print(f"Error listing/deleting files: {e}")

    return count


def main():
    parser = argparse.ArgumentParser(
        description="Delete all resources for a specific user in the OMA system"
    )
    parser.add_argument(
        "--user-id",
        type=str,
        help="User ID to filter resources (if not provided, deletes all resources)"
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Show what would be deleted without actually deleting"
    )
    parser.add_argument(
        "--resource-type",
        type=str,
        choices=[
            "all",
            "sessions",
            "agents",
            "environments",
            "vaults",
            "memory-stores",
            "skills",
            "files",
        ],
        default="all",
        help="Type of resource to delete (default: all)"
    )
    parser.add_argument(
        "--base-url",
        type=str,
        help="Base URL for the API (default: Anthropic's default, or use --localhost/--remote for convenience)"
    )
    parser.add_argument(
        "--localhost",
        action="store_true",
        help="Use localhost API (http://127.0.0.1:8787)"
    )
    parser.add_argument(
        "--remote",
        type=str,
        metavar="URL",
        help="Use remote API at specified URL (e.g., http://124.221.28.203:8787)"
    )
    
    _load_dotenv()
    args = parser.parse_args()

    # Determine base URL (default: local meta-harness)
    base_url = None
    if args.localhost:
        base_url = _DEFAULT_BASE_URL
    elif args.remote:
        base_url = args.remote
    elif args.base_url:
        base_url = args.base_url
    
    if args.dry_run:
        print("DRY RUN MODE - No resources will be deleted")
        print("=" * 60)

    resolved_base_url = (
        base_url or os.getenv("OMA_BASE_URL", _DEFAULT_BASE_URL)
    )
    print(f"Using API base URL: {resolved_base_url}")
    print("=" * 60)

    client = get_client(base_url)
    
    user_filter = f" for user {args.user_id}" if args.user_id else " for all users"
    print(f"Deleting resources{user_filter}")
    print("=" * 60)
    
    total_deleted = 0
    
    # Delete in dependency order: sessions first, then other resources
    if args.resource_type in ["all", "sessions"]:
        print("\n[1/7] Processing sessions...")
        count = delete_all_sessions(client, args.user_id, args.dry_run)
        print(f"Deleted {count} sessions")
        total_deleted += count
    
    if args.resource_type in ["all", "agents"]:
        print("\n[2/7] Processing agents...")
        count = delete_all_agents(client, args.user_id, args.dry_run)
        print(f"Archived and deleted {count} agents")
        total_deleted += count
    
    if args.resource_type in ["all", "environments"]:
        print("\n[3/7] Processing environments...")
        count = delete_all_environments(client, args.user_id, args.dry_run)
        print(f"Archived and deleted {count} environments")
        total_deleted += count
    
    if args.resource_type in ["all", "vaults"]:
        print("\n[4/7] Processing vaults...")
        count = delete_all_vaults(client, args.user_id, args.dry_run)
        print(f"Archived and deleted {count} vaults")
        total_deleted += count
    
    if args.resource_type in ["all", "memory-stores"]:
        print("\n[5/7] Processing memory stores...")
        count = delete_all_memory_stores(client, args.user_id, args.dry_run)
        print(f"Archived and deleted {count} memory stores")
        total_deleted += count
    
    if args.resource_type in ["all", "skills"]:
        print("\n[6/7] Processing skills...")
        count = delete_all_skills(client, args.user_id, args.dry_run)
        print(f"Deleted {count} skills")
        total_deleted += count

    if args.resource_type in ["all", "files"]:
        print("\n[7/7] Processing files...")
        count = delete_all_files(resolved_base_url, args.dry_run)
        print(f"Deleted {count} files")
        total_deleted += count
    
    print("\n" + "=" * 60)
    if args.dry_run:
        print(f"DRY RUN: Would delete {total_deleted} total resources")
    else:
        print(f"Successfully deleted/archived {total_deleted} total resources")


if __name__ == "__main__":
    main()
