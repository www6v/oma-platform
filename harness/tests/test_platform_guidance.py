from oma_adapter.platform_guidance import memory_platform_reminders


def test_memory_platform_reminders_include_instructions() -> None:
    reminders = memory_platform_reminders(
        [
            {
                "type": "memory_store",
                "store_id": "mst_1",
                "store_name": "user-preferences",
                "read_only": False,
                "instructions": "Write preferences under /mnt/memory/user-preferences/",
            },
        ],
    )
    assert len(reminders) == 1
    text = reminders[0]["text"]
    assert "/mnt/memory/user-preferences/" in text
    assert "read-write" in text
    assert reminders[0]["source"] == "memory:mst_1"
