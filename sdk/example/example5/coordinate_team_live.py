#!/usr/bin/env python
# -*- coding: utf-8 -*-
"""Launch coordinate_team_live (CT6 soak) with Python 3.11+."""
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
        "coordinate_team_live_main.py",
    )
    sys.exit(launch(_MAIN))
