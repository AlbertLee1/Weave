"""Pytest fixtures shared across the weave-client test suite.

These tests are written to run under either pytest+respx (the canonical setup
described in pyproject.toml) or, if those are unavailable, the stdlib unittest
runner via tests/run_unittest.py which spins up a real http.server in a thread.

Pytest fixtures only fire when pytest is actually present, so this file imports
optional dependencies lazily.
"""
from __future__ import annotations


def pytest_configure(config):  # pragma: no cover - only used under pytest
    config.addinivalue_line(
        "markers", "live: marks tests that require a live Weave server"
    )
