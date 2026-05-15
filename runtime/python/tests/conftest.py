"""pytest bootstrap that makes the ``weave_runtime`` package importable.

The runtime is not installed as a wheel for the dev/CI loop — pytest is
invoked from ``runtime/python`` directly. Prepending the package's
parent directory to ``sys.path`` lets ``from weave_runtime import ...``
resolve without a ``pip install -e .`` round-trip.
"""

from __future__ import annotations

import os
import sys

_PACKAGE_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
if _PACKAGE_ROOT not in sys.path:
    sys.path.insert(0, _PACKAGE_ROOT)
