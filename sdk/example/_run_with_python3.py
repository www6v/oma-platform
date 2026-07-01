#!/usr/bin/env python
# -*- coding: utf-8 -*-
"""Re-exec a script with Python 3.11+ when the current interpreter is too old."""
from __future__ import print_function

import os
import subprocess
import sys

MIN_VERSION = (3, 11)


def launch(main_script):
    main_script = os.path.abspath(main_script)
    if not os.path.isfile(main_script):
        print("missing script:", main_script, file=sys.stderr)
        return 1

    if sys.version_info[0] < 3 or sys.version_info < MIN_VERSION:
        py3 = os.environ.get("PYTHON3", "python3")
        cmd = [py3, main_script] + sys.argv[1:]
        return subprocess.call(cmd)

    import runpy

    runpy.run_path(main_script, run_name="__main__")
    return 0
