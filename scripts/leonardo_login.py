#!/usr/bin/env python3
"""Log in to Leonardo.Ai, capture API traffic, and export browser cookies."""

from __future__ import annotations

import argparse
import asyncio
import json
import logging
import os
import sys
import traceback
from pathlib import Path
from typing import TYPE_CHECKING, Any
from urllib.parse import urlsplit

if TYPE_CHECKING:
    from patchright.async_api import BrowserContext, Page, Request

LOGIN_URL = "https://app.leonardo.ai/auth/login"
APP_URL = "https://app.leonardo.ai/"
GRAPHQL_URL = "https://api.leonardo.ai/v1/graphql"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Log in to Leonardo.Ai and export cookies plus a sanitized API trace."
    )
    parser.add_argument("--email", default=os.getenv("LEONARDO_EMAIL"))
    parser.add_argument("--password", default=os.getenv("LEONARDO_PASSWORD"))
    parser.add_argument("--headless", action="store_true")
    parser.add_argument("--channel", default="msedge")
    parser.add_argument(
        "--proxy",
        default=os.getenv("LEONARDO_PROXY"),
        help="Browser proxy URL, for example http://127.0.0.1:7890.",
    )
    parser.add_argument(
        "--solve-captcha",
        action="store_true",
        help="Click a visible Cloudflare Turnstile challenge through Patchright.",
    )
    parser.add_argument("--timeout", type=int, default=120)
    parser.add_argument(
        "--output-dir",
        type=Path,
        default=Path("artifacts/leonardo-login"),
    )
    parser.add_argument(
        "--profile-dir",
        type=Path,
        default=Path("artifacts/leonardo-profile"),
    )
    parser.add_argument(
        "--import-cookies",
        type=Path,
        help="Seed a new browser profile with cookies exported by a prior successful login.",
    )
    args = parser.parse_args()
    return args


def request_summary(request: Request) -> dict[str, Any] | None:
    url = request.url
    host = urlsplit(url).hostname or ""
    if host not in {"app.leonardo.ai", "api.leonardo.ai"}:
        return None
    if request.resource_type not in {"fetch", "xhr", "document"}:
        return None

    operation = None
    if url == GRAPHQL_URL and request.post_data:
        try:
            operation = json.loads(request.post_data).get("operationName")
        except (json.JSONDecodeError, AttributeError):
            pass

    headers = request.headers
    return {
        "method": request.method,
        "url": url,
        "resource_type": request.resource_type,
        "operation_name": operation,
        "authenticated": "authorization" in headers,
        "schema_version": headers.get("x-leo-schema-version"),
        "team_header": "x-leonardo-team-id" in headers,
    }


async def find_email(page: Page) -> Any:
    candidates = [
        page.locator('input[type="email"]:visible'),
        page.locator('input[name="email"]:visible'),
        page.get_by_placeholder("name@host.com", exact=True),
    ]
    for locator in candidates:
        if await locator.count() == 1:
            return locator
    raise RuntimeError("email field was not found")


async def find_password(page: Page) -> Any:
    candidates = [
        page.locator('input[type="password"]:visible'),
        page.locator('input[name="password"]:visible'),
        page.get_by_placeholder("Password", exact=False),
    ]
    for locator in candidates:
        if await locator.count() == 1:
            return locator
    raise RuntimeError("password field was not found")


async def find_submit(page: Page) -> Any:
    candidates = [
        page.locator('button[type="submit"]:visible'),
        page.get_by_role("button", name="Sign in", exact=False),
        page.get_by_role("button", name="Log in", exact=False),
    ]
    for locator in candidates:
        if await locator.count() == 1:
            return locator
    raise RuntimeError("login submit button was not found")


async def continue_email(page: Page) -> None:
    button = page.get_by_role("button", name="Continue", exact=True)
    if await button.count() != 1:
        button = page.locator('button[type="submit"]:visible')
    if await button.count() != 1:
        raise RuntimeError("email continue button was not found")
    for _ in range(30):
        if await button.is_enabled():
            break
        await page.wait_for_timeout(500)
    if not await button.is_enabled():
        raise RuntimeError("email continue button remained disabled after captcha solving")
    await button.click()
    await page.locator('input[type="password"]:visible').wait_for(state="visible")


async def open_email_form(page: Page) -> None:
    if await page.locator('input[type="email"]:visible').count() == 1:
        return
    button = page.get_by_role("button", name="Continue with Email", exact=True)
    if await button.count() != 1:
        raise RuntimeError("Continue with Email button was not found")
    for _ in range(3):
        await button.click()
        try:
            await page.locator('input[type="email"]:visible').wait_for(
                state="visible", timeout=7000
            )
            return
        except Exception:
            await page.wait_for_timeout(1500)
    raise RuntimeError("email form did not open after three attempts")


async def captcha_present(page: Page) -> bool:
    responses = page.locator('input[name="cf-turnstile-response"]')
    if await responses.count() > 0:
        for index in range(await responses.count()):
            if await responses.nth(index).input_value():
                return False
        return True

    selectors = [
        'iframe[src*="challenges.cloudflare.com"]',
        'iframe[title*="Cloudflare"]',
        '.cf-turnstile',
        'script[src*="challenges.cloudflare.com/turnstile/v0"]',
    ]
    for selector in selectors:
        if await page.locator(selector).count() > 0:
            return True
    return False


async def captcha_completed(page: Page) -> bool:
    responses = page.locator('input[name="cf-turnstile-response"]')
    response_count = await responses.count()
    for index in range(response_count):
        if await responses.nth(index).input_value():
            return True
    if response_count > 0:
        return False

    buttons = page.locator('button[type="submit"]:visible')
    for index in range(await buttons.count()):
        if await buttons.nth(index).is_enabled():
            return True
    return False


async def find_turnstile_checkbox(page: Page) -> Any | None:
    for frame in page.frames:
        if frame.is_detached() or "challenges.cloudflare.com" not in frame.url:
            continue
        try:
            checkboxes = await frame.query_selector_all('input[type="checkbox"]')
        except Exception:
            continue
        for checkbox in checkboxes:
            try:
                if await checkbox.is_visible():
                    return checkbox
            except Exception:
                continue
    return None


async def solve_captcha(page: Page) -> None:
    for attempt in range(1, 4):
        logging.info("Solving Cloudflare Turnstile through Patchright, attempt %d/3", attempt)
        deadline = asyncio.get_running_loop().time() + 75
        checkbox_clicked = False
        while asyncio.get_running_loop().time() < deadline:
            if await captcha_completed(page):
                logging.info("Cloudflare Turnstile completed")
                return
            checkbox = await find_turnstile_checkbox(page)
            if checkbox is not None and not checkbox_clicked:
                try:
                    await checkbox.click()
                    checkbox_clicked = True
                    logging.info("Cloudflare Turnstile checkbox clicked")
                except Exception as exc:
                    logging.warning("Cloudflare Turnstile checkbox click failed: %s", exc)
            await page.wait_for_timeout(2000)
        if await captcha_completed(page):
            logging.info("Cloudflare Turnstile completed")
            return
        if attempt < 3:
            await page.wait_for_timeout(5000)
    raise RuntimeError("Cloudflare Turnstile did not complete after three attempts")


async def wait_for_login(page: Page, timeout_seconds: int) -> None:
    deadline = asyncio.get_running_loop().time() + timeout_seconds
    while asyncio.get_running_loop().time() < deadline:
        if "/auth/" not in page.url:
            return
        await asyncio.sleep(1)
    raise TimeoutError("login did not leave the authentication page before timeout")


async def fetch_browser_session(page: Page, force_refresh: bool = False) -> dict[str, Any]:
    """Fetch the session through Chromium, including Vercel browser checks."""
    expression = """async ({ forceRefresh }) => {
        let response = await fetch('/api/auth/get-session', {
            credentials: 'include',
            headers: { Accept: 'application/json' },
        });
        let payload = {};
        try { payload = (await response.json()) || {}; } catch (_) {}
        const initialPayload = payload;
        const initialSession = initialPayload.session || {};
        if (initialSession.accessToken && (forceRefresh || initialPayload.needsRefresh)) {
            const refreshResponse = await fetch('/api/auth/get-session', {
                method: 'POST',
                credentials: 'include',
                headers: { Accept: 'application/json' },
            });
            let refreshedPayload = {};
            try { refreshedPayload = (await refreshResponse.json()) || {}; } catch (_) {}
            if ((refreshedPayload.session || {}).accessToken) {
                response = refreshResponse;
                payload = refreshedPayload;
            }
        }
        const session = payload.session || {};
        return {
            status: response.status,
            verified: Boolean(session.accessToken),
            email: (payload.user || {}).email || null,
            access_token: session.accessToken || null,
            access_token_expiry: session.accessTokenExpiry || null,
            hasura_user_id: session.hasuraUserId || null,
            cognito_sub: session.cognitoSub || null,
            user_agent: navigator.userAgent,
        };
    }"""
    last: dict[str, Any] = {}
    for attempt in range(3):
        try:
            last = await page.evaluate(expression, {"forceRefresh": force_refresh})
        except Exception:
            return {}
        if last.get("verified") or last.get("status") != 429:
            return last
        if attempt < 2:
            await page.wait_for_timeout(2000)
    return last


async def authenticated(page: Page) -> bool:
    return bool((await fetch_browser_session(page)).get("verified"))


async def export_session(
    context: BrowserContext,
    page: Page,
    output_dir: Path,
    requests: list[dict[str, Any]],
) -> None:
    cookies = await context.cookies([APP_URL, "https://api.leonardo.ai/"])
    cookie_header = "; ".join(
        f"{cookie['name']}={cookie['value']}"
        for cookie in cookies
        if cookie["domain"].endswith("leonardo.ai")
    )
    await context.storage_state(path=output_dir / "storage-state.json")
    storage_state_path = output_dir / "storage-state.json"
    cookies_path = output_dir / "cookies.json"
    cookie_header_path = output_dir / "cookie-header.txt"
    cookies_path.write_text(json.dumps(cookies, indent=2), encoding="utf-8")
    cookie_header_path.write_text(cookie_header, encoding="utf-8")
    storage_state_path.chmod(0o600)
    cookies_path.chmod(0o600)
    cookie_header_path.chmod(0o600)

    unique_requests = list(
        {
            (
                item["method"],
                item["url"],
                item["operation_name"],
                item["schema_version"],
            ): item
            for item in requests
        }.values()
    )
    (output_dir / "api-summary.json").write_text(
        json.dumps(unique_requests, indent=2), encoding="utf-8"
    )

    browser_session = await fetch_browser_session(page, force_refresh=True)
    if not browser_session.get("verified"):
        raise RuntimeError("exported cookies did not produce an authenticated session")
    sensitive_path = output_dir / "session-token.json"
    sensitive_path.write_text(
        json.dumps(browser_session, indent=2), encoding="utf-8"
    )
    sensitive_path.chmod(0o600)
    safe_session = {
        key: value
        for key, value in browser_session.items()
        if key != "access_token"
    }
    (output_dir / "session-summary.json").write_text(
        json.dumps(safe_session, indent=2), encoding="utf-8"
    )


async def run(args: argparse.Namespace) -> None:
    output_dir = args.output_dir.resolve()
    output_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    output_dir.chmod(0o700)
    captured_requests: list[dict[str, Any]] = []

    from patchright.async_api import async_playwright as async_patchright

    async with async_patchright() as patchright:
        launch_options: dict[str, Any] = {}
        if args.proxy:
            launch_options["proxy"] = {"server": args.proxy}

        context = await patchright.chromium.launch_persistent_context(
            user_data_dir=args.profile_dir.resolve(),
            channel=args.channel or None,
            headless=args.headless,
            record_har_path=output_dir / "recording.har",
            record_har_content="omit",
            **launch_options,
        )
        if args.import_cookies:
            cookies = json.loads(args.import_cookies.read_text(encoding="utf-8"))
            await context.add_cookies(cookies)
        page = await context.new_page()

        def capture(request: Request) -> None:
            summary = request_summary(request)
            if summary:
                captured_requests.append(summary)

        page.on("request", capture)
        try:
            await page.goto(LOGIN_URL, wait_until="domcontentloaded")
            await page.wait_for_timeout(3000)
            if not await authenticated(page):
                if not args.email or not args.password:
                    raise RuntimeError(
                        "profile is not authenticated; provide LEONARDO_EMAIL and LEONARDO_PASSWORD or --import-cookies"
                    )
                await open_email_form(page)
                await (await find_email(page)).fill(args.email)
                if await captcha_present(page):
                    if not args.solve_captcha:
                        raise RuntimeError(
                            "visible Cloudflare challenge detected on email step; rerun with --solve-captcha"
                        )
                    await page.screenshot(path=output_dir / "00-email-captcha.png", full_page=True)
                    await solve_captcha(page)
                    await page.wait_for_timeout(2000)
                await continue_email(page)
                await (await find_password(page)).fill(args.password)
                await page.screenshot(path=output_dir / "01-before-sign-in.png", full_page=True)

                if await captcha_present(page):
                    if not args.solve_captcha:
                        raise RuntimeError(
                            "visible Cloudflare challenge detected; rerun with --solve-captcha"
                        )
                    await page.screenshot(path=output_dir / "03-before-captcha.png", full_page=True)
                    await solve_captcha(page)
                    await page.wait_for_timeout(2000)
                    await page.screenshot(path=output_dir / "04-after-captcha.png", full_page=True)

                submit = await find_submit(page)
                if not await submit.is_enabled():
                    raise RuntimeError("Sign In button is disabled")
                await submit.click()
                await page.wait_for_timeout(3000)
                await page.screenshot(path=output_dir / "02-after-sign-in.png", full_page=True)

                if await captcha_present(page):
                    if args.solve_captcha:
                        await page.screenshot(path=output_dir / "03-before-captcha.png", full_page=True)
                        await solve_captcha(page)
                        await page.wait_for_timeout(2000)
                        await page.screenshot(path=output_dir / "04-after-captcha.png", full_page=True)
                    elif args.headless:
                        raise RuntimeError(
                            "Cloudflare challenge appeared after submit; use headed mode to complete it"
                        )

                await wait_for_login(page, args.timeout)
            await page.goto(APP_URL, wait_until="domcontentloaded")
            await page.wait_for_timeout(5000)
            await export_session(context, page, output_dir, captured_requests)
            print(f"Login succeeded. Sensitive session artifacts: {output_dir}")
        except Exception:
            await page.screenshot(path=output_dir / "failure.png", full_page=True)
            raise
        finally:
            await context.close()


def main() -> int:
    args = parse_args()
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
    try:
        asyncio.run(run(args))
    except KeyboardInterrupt:
        return 130
    except Exception as exc:
        print(f"Login capture failed: {exc}", file=sys.stderr)
        if os.getenv("LEONARDO_LOGIN_DEBUG"):
            traceback.print_exc()
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
