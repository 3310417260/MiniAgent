#!/usr/bin/env python3

"""Safe Python demo script for MiniAgent skill runner learning.

It prints JSON about its arguments and does not read or write files.
"""

import json
import sys


def main() -> int:
    label = sys.argv[1] if len(sys.argv) > 1 else ""
    count_raw = sys.argv[2] if len(sys.argv) > 2 else "0"
    try:
        count = int(count_raw)
    except ValueError:
        print(json.dumps({"error": "count must be an integer"}))
        return 2

    print(
        json.dumps(
            {
                "script": "inspect_args.py",
                "label": label,
                "count": count,
                "argv_count": len(sys.argv) - 1,
            },
            ensure_ascii=False,
            indent=2,
        )
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
