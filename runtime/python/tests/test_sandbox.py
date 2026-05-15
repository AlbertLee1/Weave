"""TDD tests for the filesystem sandbox (VTX-049 BDD #3).

Confirm that ``install_filesystem_sandbox`` blocks reads of host files
named in the denylist (``/etc/passwd``, ``~/.ssh``, etc.) while leaving
benign reads alone. Tests undo the patch in an explicit ``try/finally``
block — the sandbox needs to come off *before* pytest's ``tmp_path``
cleanup runs, otherwise ``shutil.rmtree`` hits the guard and trips its
``onerror`` recursion limit.
"""

from __future__ import annotations

import os

import pytest

from weave_runtime.sandbox import (
    SandboxViolation,
    install_filesystem_sandbox,
    is_path_denied,
    uninstall_filesystem_sandbox,
)


@pytest.fixture(autouse=True)
def _sandbox_safety_net():
    """Belt-and-braces: if a test forgets its ``finally``, undo the
    patch anyway so subsequent tests aren't poisoned."""

    yield
    uninstall_filesystem_sandbox()


def test_is_path_denied_default_denylist_blocks_etc_passwd():
    assert is_path_denied("/etc/passwd")


def test_is_path_denied_default_denylist_blocks_dot_ssh():
    home = os.path.expanduser("~")
    assert is_path_denied(os.path.join(home, ".ssh", "id_rsa"))


def test_is_path_denied_allows_neighbour_names_outside_denylist():
    # ``/etchosts`` is a string-startswith trap; commonpath must guard
    # so neighbour prefixes don't accidentally match.
    assert not is_path_denied("/etchosts")


def test_install_then_open_etc_passwd_raises_sandboxviolation():
    """BDD #3 — the canonical case: a function tries to read
    ``/etc/passwd`` and the sandbox refuses."""

    install_filesystem_sandbox()
    try:
        with pytest.raises(SandboxViolation) as ei:
            with open("/etc/passwd") as f:
                f.read()
        assert ei.value.path.startswith("/etc")
    finally:
        uninstall_filesystem_sandbox()


def test_install_does_not_block_benign_reads_outside_denylist(tmp_path):
    target = tmp_path / "ok.txt"
    target.write_text("public")

    # Narrow denylist — ``/etc`` is not where tmp_path lives.
    install_filesystem_sandbox(denylist=["/etc"])
    try:
        with open(target) as f:
            assert f.read() == "public"
    finally:
        uninstall_filesystem_sandbox()


def test_install_blocks_open_within_custom_denylist(tmp_path):
    """A custom denylist (anchored on a path we control) lets the
    test exercise the deny branch without depending on /etc layout."""

    secret = tmp_path / "secret.txt"
    secret.write_text("classified")

    install_filesystem_sandbox(denylist=[str(tmp_path)])
    try:
        with pytest.raises(SandboxViolation) as ei:
            with open(secret) as f:
                f.read()
        assert ei.value.path == os.path.abspath(str(secret))
    finally:
        # Crucial: uninstall BEFORE pytest's tmp_path teardown runs,
        # otherwise shutil.rmtree(tmp_path) hits the guard and loops.
        uninstall_filesystem_sandbox()


def test_install_is_idempotent_when_called_twice():
    install_filesystem_sandbox(denylist=["/etc"])
    try:
        # Second call must not raise; replaces the denylist atomically.
        install_filesystem_sandbox(denylist=["/etc", "/root"])
    finally:
        uninstall_filesystem_sandbox()


def test_uninstall_restores_open(tmp_path):
    secret = tmp_path / "blocked.txt"
    secret.write_text("x")

    install_filesystem_sandbox(denylist=[str(tmp_path)])
    try:
        with pytest.raises(SandboxViolation):
            with open(secret) as f:
                f.read()
    finally:
        uninstall_filesystem_sandbox()

    # After uninstall, the same path opens normally.
    assert open(secret).read() == "x"


def test_os_open_is_also_guarded(tmp_path):
    secret = tmp_path / "blocked.txt"
    secret.write_text("x")

    install_filesystem_sandbox(denylist=[str(tmp_path)])
    try:
        with pytest.raises(SandboxViolation):
            fd = os.open(str(secret), os.O_RDONLY)
            os.close(fd)
    finally:
        uninstall_filesystem_sandbox()
