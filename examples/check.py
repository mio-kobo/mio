"""Run the native example suite with an existing Mio CLI (Python 3.12)."""

from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts/ci"))

from examples_native import main


if __name__ == "__main__":
    raise SystemExit(main())
