"""Wire OMA implementations into the pi_dynamic_workflows extension.

Called once at harness startup (after ``register_extension()``). Injects
``OmaSubAgentRunner`` and ``OmaWorkflowBootstrap`` as the concrete
backends for the workflow package's abstract Protocol interfaces.

Without this wiring:
- ``get_sub_agent_runner()`` returns ``TodoSubAgentRunner`` → workflow
  agent() steps raise ``NotImplementedError``.
- ``get_workflow_bootstrap()`` returns ``NoopWorkflowBootstrap`` → workflow
  executions run but no OMA agents/sessions are created.
"""

from __future__ import annotations

import logging

from pi_dynamic_workflows.lib.sub_agent_runner import set_sub_agent_runner
from pi_dynamic_workflows.lib.workflow_bootstrap import set_workflow_bootstrap

from oma_adapter.workflow_bootstrap import OmaWorkflowBootstrap
from oma_adapter.workflow_sub_agent_runner import OmaSubAgentRunner

logger = logging.getLogger(__name__)


def configure_workflow_oma_integration() -> None:
    """Inject OMA implementations into the workflow extension.

    Idempotent: safe to call multiple times (e.g. in tests that reset
    globals between runs).
    """
    set_sub_agent_runner(OmaSubAgentRunner())
    set_workflow_bootstrap(OmaWorkflowBootstrap())
    logger.info(
        "workflow ↔ OMA integration configured "
        "(runner=OmaSubAgentRunner, bootstrap=OmaWorkflowBootstrap)",
    )


def reset_workflow_oma_integration() -> None:
    """Clear registry back to TODO/noop defaults (tests)."""
    from pi_dynamic_workflows.lib.sub_agent_runner import reset_sub_agent_runner
    from pi_dynamic_workflows.lib.workflow_bootstrap import reset_workflow_bootstrap

    reset_sub_agent_runner()
    reset_workflow_bootstrap()
