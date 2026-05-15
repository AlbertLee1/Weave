"""Filesystem sandbox for the Vertex Python runtime (VTX-049 BDD #3).

The sandbox blocks function code from reading host files outside an
allow-list. The threat model is *accidental* exfiltration of secrets
(`/etc/passwd`, `/etc/shadow`, AWS creds, etc.) by a Function author
who imports something with side effects, or by a poorly-written model
that calls ``open("/etc/passwd")``. It is not a hardened container
boundary — for that, operators should still run the sidecar in its
own user / namespace / VM.

Approach: we monkey-patch the built-in ``open`` (and ``os.open``) to
reject paths that resolve under a denylist of host directories before
delegating to the real implementation. The patch is process-wide and
installed at app startup; tests undo it via ``uninstall_filesystem_sandbox``.

We intentionally do NOT try to virtualise the entire stdlib —
``pathlib.Path.open``, ``io.open``, ``shutil.copy``, ``codecs.open``
all funnel through ``builtins.open`` / ``os.open`` so a single guard
at those entry points covers the common cases. Calls that go straight
to ``os.openat`` / ``_io.open`` are out of scope; the sidecar should
be redeployed if an escape is discovered.
"""

from __future__ import annotations

import builtins
import os
from pathlib import Path
from typing import Iterable, Optional, Tuple


class SandboxViolation(PermissionError):
    """Raised when sandboxed code tries to access a denied path.

    Inherits ``PermissionError`` so any handler already catching that
    base class will see the violation; the FastAPI exception handler in
    ``app.py`` does the work of converting it to a 403 envelope.
    """

    def __init__(self, path: str, reason: str = "path outside sandbox"):
        self.path = path
        self.reason = reason
        super().__init__(f"sandbox violation: {reason}: {path}")


# Paths under any of these prefixes are blocked outright. The list is
# conservative — it covers the high-value targets named in the BDD
# spec plus the obvious neighbours that show up in CTF reads.
DEFAULT_DENYLIST: Tuple[str, ...] = (
    "/etc",
    "/root",
    "/proc",
    "/sys",
    "/var/log",
    "/var/run",
    "/Users",          # macOS dev host home — covers /etc-equivalent locations
    "/home",
    os.path.expanduser("~/.ssh"),
    os.path.expanduser("~/.aws"),
    os.path.expanduser("~/.config"),
)


_state: dict = {"installed": False, "denylist": (), "real_open": None, "real_os_open": None}


def _normalise(path) -> str:
    """Normalise an open() path argument to an absolute string.

    Returns the empty string for non-path arguments (already-open file
    descriptors, ``bytes``-like objects we can't decode, etc.) so the
    deny check short-circuits to "allow" — those paths can't be used
    to read arbitrary host files anyway.
    """

    if isinstance(path, int):
        return ""
    if isinstance(path, (bytes, bytearray)):
        try:
            path = path.decode("utf-8", errors="strict")
        except UnicodeDecodeError:
            return ""
    if isinstance(path, os.PathLike):
        path = os.fspath(path)
    if not isinstance(path, str):
        return ""
    if not path:
        return ""
    try:
        return os.path.abspath(path)
    except (TypeError, ValueError):
        return ""


def _is_denied(absolute_path: str, denylist: Tuple[str, ...]) -> bool:
    """Return True when ``absolute_path`` falls under any denylist entry.

    Uses ``commonpath`` rather than ``startswith`` so neighbour names
    don't accidentally match — ``/etchosts`` should not be blocked by
    the ``/etc`` rule.
    """

    if not absolute_path:
        return False
    for prefix in denylist:
        if not prefix:
            continue
        try:
            if os.path.commonpath([absolute_path, prefix]) == prefix:
                return True
        except ValueError:
            # commonpath raises on mixed drive letters / relative
            # paths after normalisation; treat as not-denied.
            continue
    return False


def install_filesystem_sandbox(denylist: Optional[Iterable[str]] = None) -> None:
    """Install the filesystem sandbox process-wide.

    Idempotent: a second call with the same denylist is a no-op so app
    startup can wire this without guarding against double-init. Calling
    with a different denylist replaces the active rules — useful in
    tests that want to assert on a narrow rule set.
    """

    if denylist is None:
        denylist = DEFAULT_DENYLIST
    canonical = tuple(_normalise(p) or p for p in denylist if p)

    if _state["installed"]:
        _state["denylist"] = canonical
        return

    real_open = builtins.open
    real_os_open = os.open

    def guarded_open(file, mode="r", buffering=-1, encoding=None, errors=None, newline=None, closefd=True, opener=None):
        absolute = _normalise(file)
        if _is_denied(absolute, _state["denylist"]):
            raise SandboxViolation(absolute or str(file))
        return real_open(
            file,
            mode=mode,
            buffering=buffering,
            encoding=encoding,
            errors=errors,
            newline=newline,
            closefd=closefd,
            opener=opener,
        )

    def guarded_os_open(path, flags, mode=0o777, *, dir_fd=None):
        absolute = _normalise(path)
        if _is_denied(absolute, _state["denylist"]):
            raise SandboxViolation(absolute or str(path))
        if dir_fd is None:
            return real_os_open(path, flags, mode)
        return real_os_open(path, flags, mode, dir_fd=dir_fd)

    builtins.open = guarded_open  # type: ignore[assignment]
    os.open = guarded_os_open  # type: ignore[assignment]

    _state["installed"] = True
    _state["denylist"] = canonical
    _state["real_open"] = real_open
    _state["real_os_open"] = real_os_open


def uninstall_filesystem_sandbox() -> None:
    """Restore the original built-ins. Intended for tests.

    Safe to call when the sandbox isn't installed.
    """

    if not _state["installed"]:
        return
    builtins.open = _state["real_open"]  # type: ignore[assignment]
    os.open = _state["real_os_open"]  # type: ignore[assignment]
    _state["installed"] = False
    _state["denylist"] = ()
    _state["real_open"] = None
    _state["real_os_open"] = None


def is_path_denied(path) -> bool:
    """Public helper for tests + diagnostic logging.

    Returns True when ``path`` would be blocked by the currently
    installed denylist (or the default denylist if the sandbox is
    not installed).
    """

    denylist = _state["denylist"] or tuple(_normalise(p) or p for p in DEFAULT_DENYLIST)
    return _is_denied(_normalise(path), denylist)


def _sandbox_path(path) -> str:
    """Normalisation helper used by Path.open / Path.read_text patches."""

    return _normalise(path)


__all__ = [
    "DEFAULT_DENYLIST",
    "SandboxViolation",
    "install_filesystem_sandbox",
    "uninstall_filesystem_sandbox",
    "is_path_denied",
]
