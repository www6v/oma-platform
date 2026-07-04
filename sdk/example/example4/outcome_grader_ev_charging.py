#!/usr/bin/env python
# -*- coding: utf-8 -*-
"""Launch outcome_grader_ev_charging with Python 3.11+."""
from __future__ import print_function

import os
import sys

_EXAMPLE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
if _EXAMPLE_DIR not in sys.path:
    sys.path.insert(0, _EXAMPLE_DIR)

from _run_with_python3 import launch

if __name__ == "__main__":
    _MAIN = os.path.join(
        os.path.dirname(__file__),
        "outcome_grader_ev_charging_main.py",
    )
    sys.exit(launch(_MAIN))
