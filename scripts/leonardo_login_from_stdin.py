#!/usr/bin/env python3
"""Run a one-time Leonardo login without exposing credentials in argv."""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
from pathlib import Path


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--login-script", type=Path, required=True)
    parser.add_argument("--profile-dir", type=Path, required=True)
    parser.add_argument("--output-dir", type=Path, required=True)
    parser.add_argument("--proxy", required=True)
    parser.add_argument("--channel", default="chrome")
    parser.add_argument("--timeout", type=int, default=120)
    parser.add_argument("--solve-captcha", action="store_true")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    payload = json.load(sys.stdin)
    email = str(payload.get("email", "")).strip()
    password = str(payload.get("password", ""))
    if not email or not password:
        raise ValueError("email and password are required")

    args.profile_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    args.output_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    args.profile_dir.chmod(0o700)
    args.output_dir.chmod(0o700)

    env = os.environ.copy()
    env["LEONARDO_EMAIL"] = email
    env["LEONARDO_PASSWORD"] = password
    payload.clear()

    command = [
        sys.executable,
        str(args.login_script),
        "--channel",
        args.channel,
        "--proxy",
        args.proxy,
        "--timeout",
        str(args.timeout),
        "--profile-dir",
        str(args.profile_dir),
        "--output-dir",
        str(args.output_dir),
    ]
    if args.solve_captcha:
        command.append("--solve-captcha")
    subprocess.run(command, env=env, check=True, timeout=args.timeout + 180)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
