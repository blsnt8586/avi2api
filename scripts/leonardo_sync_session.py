#!/usr/bin/env python3
"""Refresh a Leonardo browser session and import it into Leonardo2API."""

from __future__ import annotations

import argparse
import json
import subprocess
from pathlib import Path

import requests


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--python", required=True)
    parser.add_argument("--login-script", type=Path, required=True)
    parser.add_argument("--profile-dir", type=Path, required=True)
    parser.add_argument("--output-dir", type=Path, required=True)
    parser.add_argument("--proxy", default="")
    parser.add_argument("--base-url", required=True)
    parser.add_argument("--account-id", required=True)
    parser.add_argument("--env-file", type=Path, required=True)
    parser.add_argument("--channel", default="chrome")
    parser.add_argument("--headless", action="store_true")
    parser.add_argument("--solve-captcha", action="store_true")
    parser.add_argument("--import-cookies", type=Path)
    parser.add_argument("--login-timeout", type=int, default=120)
    parser.add_argument("--login-process-timeout", type=int, default=300)
    return parser.parse_args()


def read_env(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    for raw in path.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        values[key] = value
    return values


def cleanup_sensitive_artifacts(output_dir: Path) -> None:
    sensitive_names = {
        "session-token.json",
        "cookie-header.txt",
        "cookies.json",
        "storage-state.json",
        "recording.har",
    }
    for path in output_dir.iterdir():
        if path.name in sensitive_names or path.suffix.lower() == ".png":
            path.unlink(missing_ok=True)


def load_browser_session(output_dir: Path) -> dict[str, object]:
    browser_session = json.loads(
        (output_dir / "session-token.json").read_text(encoding="utf-8")
    )
    browser_session.pop("status", None)
    browser_session["cookie_header"] = (output_dir / "cookie-header.txt").read_text(
        encoding="utf-8"
    )
    return browser_session


def main() -> int:
    args = parse_args()
    args.output_dir.mkdir(parents=True, exist_ok=True)
    subprocess.run(
        [
            args.python,
            str(args.login_script),
            *(["--headless"] if args.headless else []),
            "--channel",
            args.channel,
            *(["--proxy", args.proxy] if args.proxy else []),
            *(["--import-cookies", str(args.import_cookies)] if args.import_cookies else []),
            *(["--solve-captcha"] if args.solve_captcha else []),
            "--timeout",
            str(args.login_timeout),
            "--profile-dir",
            str(args.profile_dir),
            "--output-dir",
            str(args.output_dir),
        ],
        check=True,
        timeout=args.login_process_timeout,
    )

    browser_session = load_browser_session(args.output_dir)
    env = read_env(args.env_file)
    sync_token = env["LEO_SESSION_SYNC_TOKEN"]

    client = requests.Session()
    response = client.put(
        f"{args.base_url.rstrip('/')}/internal/session-sync/accounts/{args.account_id}",
        headers={"Authorization": f"Bearer {sync_token}"},
        json=browser_session,
        timeout=60,
    )
    if not response.ok:
        message = f"HTTP {response.status_code}"
        try:
            payload = response.json()
            error = payload.get("error") or {}
            detail = str(error.get("message") or error.get("code") or "").strip()
            if detail:
                message = f"{message}: {detail[:300]}"
        except (ValueError, AttributeError):
            pass
        raise RuntimeError(f"session import failed: {message}")
    account = response.json()
    cleanup_sensitive_artifacts(args.output_dir)
    print(
        json.dumps(
            {
                "account_id": account["id"],
                "status": account["status"],
                "access_token_expires_at": account["access_token_expires_at"],
                "subscription_tokens": account["subscription_tokens"],
            }
        )
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
