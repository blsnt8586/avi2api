#!/usr/bin/env python3
"""Lease and execute browser-only Leonardo session refresh jobs."""

from __future__ import annotations

import argparse
import json
import os
import shutil
import signal
import socket
import subprocess
import threading
import time
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from typing import Any

import requests


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--python", required=True)
    parser.add_argument("--sync-script", type=Path, required=True)
    parser.add_argument("--login-script", type=Path, required=True)
    parser.add_argument("--profile-root", type=Path, required=True)
    parser.add_argument("--output-root", type=Path, required=True)
    parser.add_argument("--base-url", required=True)
    parser.add_argument("--env-file", type=Path, required=True)
    parser.add_argument("--proxy", default="")
    parser.add_argument("--channel", default="chrome")
    parser.add_argument("--concurrency", type=int, default=3)
    parser.add_argument("--worker-group", default="default")
    parser.add_argument("--idle-poll", type=float, default=2.0)
    parser.add_argument("--heartbeat", type=float, default=60.0)
    parser.add_argument("--solve-captcha", action="store_true")
    parser.add_argument("--headless-refresh", action="store_true")
    parser.add_argument("--headless-channel", default="chrome")
    parser.add_argument("--headless-login-timeout", type=int, default=60)
    parser.add_argument("--headless-process-timeout", type=int, default=120)
    return parser.parse_args()


def read_env(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    for raw in path.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if line and not line.startswith("#") and "=" in line:
            key, value = line.split("=", 1)
            values[key] = value
    return values


def cookie_header_to_patchright(cookie_header: str) -> list[dict[str, str]]:
    cookies: list[dict[str, str]] = []
    for raw_cookie in cookie_header.split(";"):
        name, separator, value = raw_cookie.strip().partition("=")
        if not separator or not name or name.startswith("$"):
            continue
        cookies.append(
            {
                "name": name,
                "value": value,
                "url": "https://app.leonardo.ai",
            }
        )
    return cookies


def write_private_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
        json.dump(value, handle)
    os.chmod(path, 0o600)


def build_sync_command(
    args: argparse.Namespace,
    account_id: str,
    profile_dir: Path,
    output_dir: Path,
    proxy: str,
    *,
    channel: str,
    headless: bool,
    solve_captcha: bool,
    import_cookies: Path | None,
    login_timeout: int,
    process_timeout: int,
) -> list[str]:
    command = [
        args.python,
        str(args.sync_script),
        "--python",
        args.python,
        "--login-script",
        str(args.login_script),
        "--channel",
        channel,
        "--profile-dir",
        str(profile_dir),
        "--output-dir",
        str(output_dir),
        "--base-url",
        args.base_url,
        "--account-id",
        account_id,
        "--env-file",
        str(args.env_file),
        "--login-timeout",
        str(login_timeout),
        "--login-process-timeout",
        str(process_timeout),
    ]
    if proxy:
        command.extend(["--proxy", proxy])
    if headless:
        command.append("--headless")
    if solve_captcha:
        command.append("--solve-captcha")
    if import_cookies is not None:
        command.extend(["--import-cookies", str(import_cookies)])
    return command


class SessionWorker:
    def __init__(self, args: argparse.Namespace) -> None:
        if args.concurrency < 1 or args.concurrency > 64:
            raise ValueError("concurrency must be between 1 and 64")
        if (
            args.headless_login_timeout < 10
            or args.headless_process_timeout < args.headless_login_timeout
        ):
            raise ValueError("headless timeouts are invalid")
        self.args = args
        self.stop = threading.Event()
        self.token = read_env(args.env_file)["LEO_SESSION_SYNC_TOKEN"]
        self.headers = {"Authorization": f"Bearer {self.token}"}
        args.profile_root.mkdir(parents=True, exist_ok=True)
        args.output_root.mkdir(parents=True, exist_ok=True)

    def endpoint(self, path: str) -> str:
        return f"{self.args.base_url.rstrip('/')}{path}"

    def run_slot(self, slot: int) -> None:
        worker_id = f"{socket.gethostname()}:{os.getpid()}:{slot}"
        client = requests.Session()
        client.headers.update(self.headers)
        child_env = os.environ.copy()
        child_env.pop("LEONARDO_EMAIL", None)
        child_env.pop("LEONARDO_PASSWORD", None)

        while not self.stop.is_set():
            try:
                response = client.post(
                    self.endpoint("/internal/session-refresh/claim"),
                    json={
                        "worker_id": worker_id,
                        "worker_group": self.args.worker_group,
                        "headless_refresh": self.args.headless_refresh,
                        "headed_login": True,
                    },
                    timeout=30,
                )
                if response.status_code == 204:
                    self.stop.wait(self.args.idle_poll)
                    continue
                response.raise_for_status()
                payload = response.json()
            except requests.RequestException as exc:
                print(f"session worker claim failed: {type(exc).__name__}", flush=True)
                self.stop.wait(max(2.0, self.args.idle_poll))
                continue

            self.execute_job(client, child_env, payload)

    def execute_job(
        self,
        client: requests.Session,
        child_env: dict[str, str],
        payload: dict[str, Any],
    ) -> None:
        job = payload["job"]
        job_id = str(job["id"])
        account_id = str(job["account_id"])
        lease_token = str(job["lease_token"])
        profile_key = str(payload["browser_profile_key"])
        profile_dir = self.args.profile_root / profile_key
        output_dir = self.args.output_root / job_id
        proxy = str(payload.get("proxy_url") or self.args.proxy)
        started = time.monotonic()
        mode = "headed"
        try:
            output_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
            cookie_header = str(payload.get("cookie_header") or "")
            cookies = cookie_header_to_patchright(cookie_header)
            if self.args.headless_refresh and cookies:
                cookie_path = output_dir / "headless-cookies.json"
                headless_output = output_dir / "headless"
                headless_profile = output_dir / "headless-profile"
                write_private_json(cookie_path, cookies)
                headless_command = build_sync_command(
                    self.args,
                    account_id,
                    headless_profile,
                    headless_output,
                    proxy,
                    channel=self.args.headless_channel,
                    headless=True,
                    solve_captcha=False,
                    import_cookies=cookie_path,
                    login_timeout=self.args.headless_login_timeout,
                    process_timeout=self.args.headless_process_timeout,
                )
                try:
                    return_code = self.run_command(
                        client,
                        child_env,
                        headless_command,
                        job_id,
                        lease_token,
                    )
                except OSError as exc:
                    return_code = -1
                    print(
                        json.dumps(
                            {
                                "job_id": job_id,
                                "account_id": account_id,
                                "mode": "headless",
                                "status": "fallback",
                                "error": f"{type(exc).__name__}: {str(exc)[:300]}",
                            }
                        ),
                        flush=True,
                    )
                if return_code == 0:
                    mode = "headless"
                else:
                    print(
                        json.dumps(
                            {
                                "job_id": job_id,
                                "account_id": account_id,
                                "mode": "headless",
                                "status": "fallback",
                                "exit_code": return_code,
                            }
                        ),
                        flush=True,
                    )
                    shutil.rmtree(headless_output, ignore_errors=True)
                    shutil.rmtree(headless_profile, ignore_errors=True)
                    cookie_path.unlink(missing_ok=True)

            if mode == "headed":
                login_email = str(payload.get("login_email") or "")
                login_password = str(payload.get("login_password") or "")
                has_login_credential = bool(login_email and login_password)
                headed_profile = (
                    output_dir / "headed-profile"
                    if has_login_credential
                    else profile_dir
                )
                headed_command = build_sync_command(
                    self.args,
                    account_id,
                    headed_profile,
                    output_dir / "headed",
                    proxy,
                    channel=self.args.channel,
                    headless=False,
                    solve_captcha=self.args.solve_captcha,
                    import_cookies=None,
                    login_timeout=120,
                    process_timeout=300,
                )
                headed_env = child_env.copy()
                if has_login_credential:
                    headed_env["LEONARDO_EMAIL"] = login_email
                    headed_env["LEONARDO_PASSWORD"] = login_password
                try:
                    return_code = self.run_command(
                        client,
                        headed_env,
                        headed_command,
                        job_id,
                        lease_token,
                    )
                finally:
                    headed_env.pop("LEONARDO_EMAIL", None)
                    headed_env.pop("LEONARDO_PASSWORD", None)
                if return_code != 0:
                    raise RuntimeError(f"headed browser refresh exited with code {return_code}")

            completed = client.post(
                self.endpoint(f"/internal/session-refresh/jobs/{job_id}/complete"),
                json={
                    "lease_token": lease_token,
                    "duration_ms": int((time.monotonic() - started) * 1000),
                },
                timeout=30,
            )
            completed.raise_for_status()
            print(
                json.dumps(
                    {
                        "job_id": job_id,
                        "account_id": account_id,
                        "mode": mode,
                        "status": "succeeded",
                    }
                ),
                flush=True,
            )
        except Exception as exc:
            message = f"{type(exc).__name__}: {str(exc)[:700]}"
            try:
                failed = client.post(
                    self.endpoint(f"/internal/session-refresh/jobs/{job_id}/fail"),
                    json={"lease_token": lease_token, "error": message},
                    timeout=30,
                )
                failed.raise_for_status()
            except requests.RequestException:
                pass
            print(
                json.dumps(
                    {
                        "job_id": job_id,
                        "account_id": account_id,
                        "status": "failed",
                        "error": message,
                    }
                ),
                flush=True,
            )
        finally:
            shutil.rmtree(output_dir, ignore_errors=True)

    def run_command(
        self,
        client: requests.Session,
        child_env: dict[str, str],
        command: list[str],
        job_id: str,
        lease_token: str,
    ) -> int:
        process = subprocess.Popen(command, env=child_env)
        try:
            next_heartbeat = time.monotonic() + self.args.heartbeat
            while process.poll() is None:
                if self.stop.wait(1.0):
                    process.terminate()
                    raise RuntimeError("worker is shutting down")
                if time.monotonic() < next_heartbeat:
                    continue
                heartbeat = client.post(
                    self.endpoint(f"/internal/session-refresh/jobs/{job_id}/heartbeat"),
                    json={"lease_token": lease_token},
                    timeout=30,
                )
                heartbeat.raise_for_status()
                next_heartbeat = time.monotonic() + self.args.heartbeat
            return process.wait(timeout=30)
        except Exception:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=10)
            raise

    def run(self) -> int:
        with ThreadPoolExecutor(max_workers=self.args.concurrency) as pool:
            futures = [pool.submit(self.run_slot, slot + 1) for slot in range(self.args.concurrency)]
            for future in futures:
                future.result()
        return 0


def main() -> int:
    worker = SessionWorker(parse_args())

    def stop(_signum: int, _frame: object) -> None:
        worker.stop.set()

    signal.signal(signal.SIGINT, stop)
    signal.signal(signal.SIGTERM, stop)
    return worker.run()


if __name__ == "__main__":
    raise SystemExit(main())
