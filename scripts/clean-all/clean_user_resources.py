#!/usr/bin/env python3
"""
Script to delete all resources for a specific user in the OMA system.

This script uses the OMA SDK to delete all resources associated with a user,
including agents, sessions, environments, vaults, memory stores, and skills.
"""

import argparse
import os
import sys
from typing import Optional

import anthropic


def get_client(base_url: Optional[str] = None) -> anthropic.Anthropic:
    """Initialize and return the Anthropic client."""
    api_key = os.getenv("ANTHROPIC_API_KEY")
    if not api_key:
        raise ValueError("ANTHROPIC_API_KEY environment variable is not set")
    
    if base_url:
        return anthropic.Anthropic(api_key=api_key, base_url=base_url)
    return anthropic.Anthropic(api_key=api_key)


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
        page = client.beta.agents.list()
        agents = list(page)
        
        for agent in agents:
            # Filter by user_id if provided (assuming agent has user metadata)
            if user_id and hasattr(agent, 'user_id') and agent.user_id != user_id:
                continue
                
            print(f"{'[DRY RUN] ' if dry_run else ''}Archiving and deleting agent {agent.id} ({agent.name})")
            if not dry_run:
                try:
                    # First archive the agent
                    client.beta.agents.archive(agent.id)
                    # Then delete it permanently
                    try:
                        client.beta.agents.delete(agent.id)
                    except AttributeError:
                        # If delete method doesn't exist, archive is sufficient
                        print(f"  Note: Delete method not available, agent archived only")
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
        choices=["all", "sessions", "agents", "environments", "vaults", "memory-stores", "skills"],
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
    
    args = parser.parse_args()
    
    # Determine base URL
    base_url = None
    if args.localhost:
        base_url = "http://127.0.0.1:8787"
    elif args.remote:
        base_url = args.remote
    elif args.base_url:
        base_url = args.base_url
    
    if args.dry_run:
        print("DRY RUN MODE - No resources will be deleted")
        print("=" * 60)
    
    if base_url:
        print(f"Using API base URL: {base_url}")
        print("=" * 60)
    
    try:
        client = get_client(base_url)
    except ValueError as e:
        print(f"Error: {e}")
        sys.exit(1)
    
    user_filter = f" for user {args.user_id}" if args.user_id else " for all users"
    print(f"Deleting resources{user_filter}")
    print("=" * 60)
    
    total_deleted = 0
    
    # Delete in dependency order: sessions first, then other resources
    if args.resource_type in ["all", "sessions"]:
        print("\n[1/6] Processing sessions...")
        count = delete_all_sessions(client, args.user_id, args.dry_run)
        print(f"Deleted {count} sessions")
        total_deleted += count
    
    if args.resource_type in ["all", "agents"]:
        print("\n[2/6] Processing agents...")
        count = delete_all_agents(client, args.user_id, args.dry_run)
        print(f"Archived and deleted {count} agents")
        total_deleted += count
    
    if args.resource_type in ["all", "environments"]:
        print("\n[3/6] Processing environments...")
        count = delete_all_environments(client, args.user_id, args.dry_run)
        print(f"Archived and deleted {count} environments")
        total_deleted += count
    
    if args.resource_type in ["all", "vaults"]:
        print("\n[4/6] Processing vaults...")
        count = delete_all_vaults(client, args.user_id, args.dry_run)
        print(f"Archived and deleted {count} vaults")
        total_deleted += count
    
    if args.resource_type in ["all", "memory-stores"]:
        print("\n[5/6] Processing memory stores...")
        count = delete_all_memory_stores(client, args.user_id, args.dry_run)
        print(f"Archived and deleted {count} memory stores")
        total_deleted += count
    
    if args.resource_type in ["all", "skills"]:
        print("\n[6/6] Processing skills...")
        count = delete_all_skills(client, args.user_id, args.dry_run)
        print(f"Deleted {count} skills")
        total_deleted += count
    
    print("\n" + "=" * 60)
    if args.dry_run:
        print(f"DRY RUN: Would delete {total_deleted} total resources")
    else:
        print(f"Successfully deleted/archived {total_deleted} total resources")


if __name__ == "__main__":
    main()
