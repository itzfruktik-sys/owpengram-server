#!/usr/bin/env python3
"""Reports which Python packages server-panel.py needs are missing or too old.

Used by owpengram-server.sh / owpengram-server.bat before launching the
panel, so both launchers share one source of truth for the dependency list
instead of duplicating it in shell and batch syntax.

A plain `importlib.import_module` presence check isn't enough: on Debian/
Ubuntu, `apt`'s python3-textual (and similar system packages) can be old
enough to import fine but lack symbols server-panel.py needs (e.g. the
`work` decorator, added well after the ancient version some distros still
ship) -- the import check passes, then the panel crashes with an
ImportError of its own a few lines into startup. Checking the installed
version against requirements-panel.txt's floor catches that upfront, with
the same actionable message as a genuinely missing package.

Prints one missing/outdated package's pip install spec per line. Exit code
is 0 if everything installed satisfies its minimum version, 1 otherwise.
"""
import importlib
import importlib.metadata
import sys

REQUIRED = [
    ("textual", "textual", "0.60"),
    ("psutil", "psutil", "5.9"),
    ("cryptography", "cryptography", "41.0"),
]


def _version_tuple(version: str) -> tuple[int, ...]:
    """Parses the leading dotted-numeric run of a version string.

    Good enough for the plain "major.minor[.patch]" versions in
    requirements-panel.txt and on PyPI -- not a full PEP 440 parser, but this
    script only ever compares against floors from that one file.
    """
    parts = []
    for chunk in version.split("."):
        digits = ""
        for ch in chunk:
            if not ch.isdigit():
                break
            digits += ch
        parts.append(int(digits) if digits else 0)
    return tuple(parts)


def main() -> int:
    problems = []
    for module_name, pip_name, min_version in REQUIRED:
        try:
            importlib.import_module(module_name)
        except ImportError:
            problems.append(f"{pip_name}>={min_version}")
            continue
        try:
            installed = importlib.metadata.version(pip_name)
        except importlib.metadata.PackageNotFoundError:
            # Importable but no dist-info to check (unusual outside a system
            # package with a broken/missing manifest) -- can't verify the
            # version, so don't block on a guess either way.
            continue
        if _version_tuple(installed) < _version_tuple(min_version):
            problems.append(f"{pip_name}>={min_version} (found {installed})")

    for name in problems:
        print(name)

    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
