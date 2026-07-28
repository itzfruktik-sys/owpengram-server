#!/usr/bin/env python3
"""Reports which Python packages server-panel.py needs are missing.

Used by owpengram-server.sh / owpengram-server.bat before launching the
panel, so both launchers share one source of truth for the dependency list
instead of duplicating it in shell and batch syntax.

Prints one missing package's pip install name per line. Exit code is 0 if
everything is already installed, 1 if anything is missing.
"""
import importlib
import sys

REQUIRED = [
    ("textual", "textual"),
    ("psutil", "psutil"),
    ("cryptography", "cryptography"),
]


def main() -> int:
    missing = []
    for module_name, pip_name in REQUIRED:
        try:
            importlib.import_module(module_name)
        except ImportError:
            missing.append(pip_name)

    for name in missing:
        print(name)

    return 1 if missing else 0


if __name__ == "__main__":
    sys.exit(main())
