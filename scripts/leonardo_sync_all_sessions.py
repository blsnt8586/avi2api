#!/usr/bin/env python3
"""Refresh multiple Leonardo browser profiles sequentially."""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import time
from pathlib import Path


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", type=Path, required=True)
    parser.add_argument("--python", required=True)
    parser.add_argument("--sync-script", type=Path, required=True)
    parser.add_argument("--login-script", type=Path, required=True)
    parser.add_argument("--proxy", required=True)
    parser.add_argument("--base-url", required=True)
    parser.add_argument("--env-file", type=Path, required=True)
    parser.add_argument("--channel", default="chrome")
    parser.add_argument("--solve-captcha", action="store_true")
    parser.add_argument("--delay", type=int, default=20)
    return parser.parse_args()


def load_accounts(path: Path) -> list[dict[str, str]]:
    accounts = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(accounts, list) or not accounts:
        raise ValueError("sync config must contain at least one account")
    required = {"account_id", "profile_dir", "output_dir"}
    seen_ids: set[str] = set()
    seen_profiles: set[str] = set()
    for account in accounts:
        if not isinstance(account, dict) or not required.issubset(account):
            raise ValueError("each sync account requires account_id, profile_dir and output_dir")
        account_id = str(account["account_id"])
        profile_dir = str(account["profile_dir"])
        if account_id in seen_ids or profile_dir in seen_profiles:
            raise ValueError("account IDs and profile directories must be unique")
        seen_ids.add(account_id)
        seen_profiles.add(profile_dir)
    return accounts


def main() -> int:
    args = parse_args()
    accounts = load_accounts(args.config)
    child_env = os.environ.copy()
    child_env.pop("LEONARDO_EMAIL", None)
    child_env.pop("LEONARDO_PASSWORD", None)
    failures: list[str] = []

    for index, account in enumerate(accounts):
        command = [
            args.python,
            str(args.sync_script),
            "--python",
            args.python,
            "--login-script",
            str(args.login_script),
            "--channel",
            args.channel,
            "--proxy",
            args.proxy,
            "--profile-dir",
            str(account["profile_dir"]),
            "--output-dir",
            str(account["output_dir"]),
            "--base-url",
            args.base_url,
            "--account-id",
            str(account["account_id"]),
            "--env-file",
            str(args.env_file),
        ]
        if args.solve_captcha:
            command.append("--solve-captcha")
        try:
            subprocess.run(command, env=child_env, check=True, timeout=360)
        except (subprocess.CalledProcessError, subprocess.TimeoutExpired) as exc:
            failures.append(str(account["account_id"]))
            print(f"session sync failed for account {account['account_id']}: {exc}")
        if index + 1 < len(accounts) and args.delay > 0:
            time.sleep(args.delay)

    if failures:
        print(f"session sync completed with {len(failures)} failure(s)")
        return 1
    print(f"session sync completed for {len(accounts)} account(s)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
