import os
import stat
import subprocess
import sys
from pathlib import Path


def main():
    binary_name = "__BIN_NAME__" + (".exe" if sys.platform == "win32" else "")
    binary = Path(__file__).resolve().parent / "bin" / binary_name

    if not binary.is_file():
        print(
            f"__BIN_NAME__: binary not found at {binary}\n"
            f"try reinstalling: pip install __BIN_NAME__",
            file=sys.stderr,
        )
        sys.exit(1)

    if sys.platform != "win32":
        m = binary.stat().st_mode
        if not (m & stat.S_IXUSR):
            binary.chmod(m | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)

    b = str(binary)
    if sys.platform == "win32":
        proc = subprocess.run([b, *sys.argv[1:]])
        sys.exit(proc.returncode)
    else:
        os.execv(b, [b, *sys.argv[1:]])
