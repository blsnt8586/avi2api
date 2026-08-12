#!/usr/bin/env python3
"""Log in to Leonardo.Ai, capture API traffic, and export browser cookies."""

from __future__ import annotations

import argparse
import asyncio
import json
import logging
import os
import re
import sys
import traceback
from datetime import datetime, timezone
from pathlib import Path
from typing import TYPE_CHECKING, Any
from urllib.parse import urlsplit

if TYPE_CHECKING:
    from patchright.async_api import BrowserContext, Page, Request, Response

LOGIN_URL = "https://app.leonardo.ai/auth/login"
APP_URL = "https://app.leonardo.ai/"
GRAPHQL_URL = "https://api.leonardo.ai/v1/graphql"
AUTH_ERROR_FIELDS = {
    "code",
    "detail",
    "error",
    "error_code",
    "error_description",
    "message",
    "reason",
    "status",
    "status_code",
}
EMAIL_PATTERN = re.compile(r"\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b", re.IGNORECASE)
JWT_PATTERN = re.compile(r"\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}(?:\.[A-Za-z0-9_-]{8,})?\b")
LONG_TOKEN_PATTERN = re.compile(r"(?<![A-Za-z0-9_-])[A-Za-z0-9_-]{48,}(?![A-Za-z0-9_-])")


class LoginAuthenticationError(RuntimeError):
    def __init__(self, code: str, message: str) -> None:
        super().__init__(message)
        self.code = code


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
        "url": f"{urlsplit(url).scheme}://{host}{urlsplit(url).path}",
        "resource_type": request.resource_type,
        "operation_name": operation,
        "authenticated": "authorization" in headers,
        "schema_version": headers.get("x-leo-schema-version"),
        "team_header": "x-leonardo-team-id" in headers,
    }


def sanitize_text(value: object, secrets: tuple[str, ...] = ()) -> str:
    text = " ".join(str(value or "").split())
    for secret in secrets:
        if secret:
            text = text.replace(secret, "[redacted-secret]")
    text = EMAIL_PATTERN.sub("[redacted-email]", text)
    text = JWT_PATTERN.sub("[redacted-token]", text)
    text = LONG_TOKEN_PATTERN.sub("[redacted-token]", text)
    return text[:500]


def safe_error_fields(
    value: object,
    secrets: tuple[str, ...] = (),
    prefix: str = "",
) -> list[dict[str, str]]:
    fields: list[dict[str, str]] = []
    if isinstance(value, dict):
        for key, child in value.items():
            normalized = str(key).lower()
            path = f"{prefix}.{normalized}" if prefix else normalized
            if normalized in AUTH_ERROR_FIELDS and isinstance(child, (str, int, float, bool)):
                sanitized = sanitize_text(child, secrets)
                if sanitized:
                    fields.append({"field": path, "value": sanitized})
            elif isinstance(child, (dict, list)):
                fields.extend(safe_error_fields(child, secrets, path))
    elif isinstance(value, list):
        for index, child in enumerate(value[:10]):
            fields.extend(safe_error_fields(child, secrets, f"{prefix}[{index}]"))
    return fields[:20]


async def auth_response_summary(
    response: Response,
    secrets: tuple[str, ...] = (),
) -> dict[str, Any] | None:
    split = urlsplit(response.url)
    request = response.request
    is_auth = split.hostname == "app.leonardo.ai" and split.path.startswith("/api/auth/")
    is_document = (
        split.hostname == "app.leonardo.ai" and request.resource_type == "document"
    )
    if not is_auth and not is_document:
        return None

    try:
        headers = await response.all_headers()
    except Exception:
        headers = {}
    content_type = str(headers.get("content-type") or "").split(";", 1)[0]
    summary: dict[str, Any] = {
        "method": request.method,
        "path": split.path,
        "resource_type": request.resource_type,
        "status": response.status,
        "content_type": content_type,
        "set_cookie": "set-cookie" in headers,
    }
    if not is_auth or split.path.endswith("/get-session"):
        return summary

    try:
        payload = await response.json()
    except Exception:
        return summary
    if isinstance(payload, dict):
        summary["body_keys"] = sorted(str(key) for key in payload)[:30]
        for key in ("success", "redirect"):
            if isinstance(payload.get(key), bool):
                summary[key] = payload[key]
    error_fields = safe_error_fields(payload, secrets)
    if error_fields:
        summary["error_fields"] = error_fields
    return summary


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
    return not await captcha_present(page)


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


def response_rejected_authentication(responses: list[dict[str, Any]]) -> bool:
    for response in responses:
        if not str(response.get("path") or "").startswith("/api/auth/"):
            continue
        if str(response.get("path") or "").endswith("/get-session"):
            continue
        if int(response.get("status") or 0) >= 400 or response.get("success") is False:
            if auth_response_error_code(response) == "CANVA_SSO_LOCK":
                continue
            return True
        for field in response.get("error_fields") or []:
            path = str(field.get("field") or "").lower()
            segments = {segment.split("[", 1)[0] for segment in path.split(".")}
            if segments.intersection({"error", "error_code", "error_description", "reason"}):
                return True
    return False


def auth_response_error_code(response: dict[str, Any]) -> str:
    for field in response.get("error_fields") or []:
        if str(field.get("field") or "").lower().split(".")[-1] == "code":
            return str(field.get("value") or "").strip().upper()
    return ""


def canva_sso_required(responses: list[dict[str, Any]]) -> bool:
    return any(auth_response_error_code(response) == "CANVA_SSO_LOCK" for response in responses)


async def first_visible(locator: Any) -> Any | None:
    for index in range(await locator.count()):
        candidate = locator.nth(index)
        try:
            if await candidate.is_visible():
                return candidate
        except Exception:
            continue
    return None


async def wait_for_canva_login(page: Page, timeout_seconds: int = 30) -> None:
    deadline = asyncio.get_running_loop().time() + timeout_seconds
    while asyncio.get_running_loop().time() < deadline:
        if (urlsplit(page.url).hostname or "").endswith("canva.com"):
            email_button = page.get_by_role(
                "button",
                name=re.compile(r"continue with email|use email|使用邮箱登录|邮箱登录", re.I),
            )
            if await first_visible(email_button) is not None:
                return
        await page.wait_for_timeout(1000)
    raise LoginAuthenticationError(
        "canva_sso_navigation_failed",
        "Leonardo required Canva SSO but the Canva login page did not become ready",
    )


async def complete_canva_sso(
    page: Page,
    email: str,
    password: str,
    solve_captcha_enabled: bool,
) -> None:
    await wait_for_canva_login(page)
    email_button = await first_visible(
        page.get_by_role(
            "button",
            name=re.compile(r"continue with email|use email|使用邮箱登录|邮箱登录", re.I),
        )
    )
    if email_button is None:
        raise LoginAuthenticationError(
            "canva_email_login_unavailable",
            "Canva SSO did not expose email login",
        )
    await email_button.click()

    email_input = page.locator(
        'input[type="email"]:visible, '
        'input[name="username"][autocomplete*="username"]:visible'
    )
    try:
        await email_input.first.wait_for(state="visible", timeout=15000)
    except Exception as exc:
        raise LoginAuthenticationError(
            "canva_email_step_unavailable",
            "Canva SSO email field did not become available",
        ) from exc
    await email_input.first.fill(email)
    continue_button = await first_visible(
        page.get_by_role(
            "button",
            name=re.compile(r"^continue$|^继续$", re.I),
        )
    )
    if continue_button is None:
        raise LoginAuthenticationError(
            "canva_email_continue_unavailable",
            "Canva SSO email step did not expose a continue button",
        )
    await continue_button.click()

    deadline = asyncio.get_running_loop().time() + 30
    password_input = page.locator('input[type="password"]:visible')
    while asyncio.get_running_loop().time() < deadline:
        if (urlsplit(page.url).hostname or "").endswith("leonardo.ai"):
            return
        if await password_input.count() > 0:
            break
        outline = await collect_form_outline(page, (email, password))
        input_names = " ".join(
            " ".join(item.values()) for item in outline.get("inputs", [])
        ).lower()
        button_text = " ".join(
            " ".join(item.values()) for item in outline.get("buttons", [])
        ).lower()
        page_text = " ".join(await collect_visible_messages(page, (email, password))).lower()
        if any(marker in input_names for marker in ("one-time-code", "verification", "otp")):
            raise LoginAuthenticationError(
                "canva_additional_verification_required",
                "Canva SSO requires a verification code",
            )
        if any(marker in page_text for marker in ("verification code", "check your email", "验证码", "查收")):
            raise LoginAuthenticationError(
                "canva_additional_verification_required",
                "Canva SSO requires email verification",
            )
        if any(marker in button_text for marker in ("create account", "创建账户", "sign up", "注册")):
            raise LoginAuthenticationError(
                "canva_account_not_found",
                "Canva SSO opened account registration instead of an existing account login",
            )
        await page.wait_for_timeout(1000)
    if await password_input.count() == 0:
        raise LoginAuthenticationError(
            "canva_login_step_unknown",
            "Canva SSO did not expose a supported password or verification step",
        )

    await password_input.first.fill(password)
    if await captcha_present(page):
        if not solve_captcha_enabled:
            raise LoginAuthenticationError(
                "canva_captcha_required",
                "Canva SSO requires a visible Cloudflare challenge",
            )
        await solve_captcha(page)
        await page.wait_for_timeout(2000)
    submit = await first_visible(page.locator('button[type="submit"]:visible'))
    if submit is None:
        submit = await first_visible(
            page.get_by_role(
                "button",
                name=re.compile(r"continue|log in|sign in|继续|登录", re.I),
            )
        )
    if submit is None:
        raise LoginAuthenticationError(
            "canva_password_submit_unavailable",
            "Canva SSO password step did not expose a submit button",
        )
    await submit.click()


async def wait_for_authenticated_login(
    page: Page,
    timeout_seconds: int,
    auth_responses: list[dict[str, Any]],
) -> dict[str, Any]:
    deadline = asyncio.get_running_loop().time() + timeout_seconds
    left_auth_checks = 0
    while asyncio.get_running_loop().time() < deadline:
        session = await fetch_browser_session(page)
        if session.get("verified"):
            return session
        if response_rejected_authentication(auth_responses):
            raise LoginAuthenticationError(
                "authentication_rejected",
                "Leonardo login endpoint rejected authentication",
            )
        if "/auth/" not in page.url:
            left_auth_checks += 1
            if left_auth_checks >= 5:
                raise LoginAuthenticationError(
                    "authenticated_session_missing",
                    "Leonardo login navigation completed without an authenticated session",
                )
        else:
            left_auth_checks = 0
        await page.wait_for_timeout(2000)
    raise LoginAuthenticationError(
        "authentication_timeout",
        "Leonardo login did not produce an authenticated session before timeout",
    )


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


async def collect_visible_messages(
    page: Page,
    secrets: tuple[str, ...],
) -> list[str]:
    expression = """() => {
        const selectors = [
            '[role="alert"]',
            '[aria-live="assertive"]',
            '[data-sonner-toast]',
            '[class*="error" i]'
        ];
        const values = [];
        for (const selector of selectors) {
            for (const element of document.querySelectorAll(selector)) {
                const style = getComputedStyle(element);
                const text = (element.innerText || element.textContent || '').trim();
                if (text && style.display !== 'none' && style.visibility !== 'hidden') {
                    values.push(text);
                }
            }
        }
        return [...new Set(values)].slice(0, 10);
    }"""
    try:
        values = await page.evaluate(expression)
    except Exception:
        return []
    return [message for value in values if (message := sanitize_text(value, secrets))]


async def collect_form_outline(
    page: Page,
    secrets: tuple[str, ...],
) -> dict[str, list[dict[str, str]]]:
    expression = """() => {
        const visible = (element) => {
            const style = getComputedStyle(element);
            return style.display !== 'none' && style.visibility !== 'hidden';
        };
        return {
            inputs: [...document.querySelectorAll('input')]
                .filter(visible)
                .slice(0, 20)
                .map((element) => ({
                    type: element.type || '',
                    name: element.name || '',
                    autocomplete: element.autocomplete || '',
                    placeholder: element.placeholder || '',
                    aria_label: element.getAttribute('aria-label') || ''
                })),
            buttons: [...document.querySelectorAll('button')]
                .filter(visible)
                .slice(0, 20)
                .map((element) => ({
                    type: element.type || '',
                    text: (element.innerText || element.textContent || '').trim()
                }))
        };
    }"""
    try:
        outline = await page.evaluate(expression)
    except Exception:
        return {"inputs": [], "buttons": []}
    if not isinstance(outline, dict):
        return {"inputs": [], "buttons": []}
    sanitized: dict[str, list[dict[str, str]]] = {"inputs": [], "buttons": []}
    for section in sanitized:
        values = outline.get(section)
        if not isinstance(values, list):
            continue
        for item in values[:20]:
            if not isinstance(item, dict):
                continue
            sanitized[section].append(
                {
                    str(key): sanitize_text(value, secrets)
                    for key, value in item.items()
                    if sanitize_text(value, secrets)
                }
            )
    return sanitized


async def write_failure_diagnostic(
    context: BrowserContext,
    page: Page,
    output_dir: Path,
    auth_responses: list[dict[str, Any]],
    error: Exception,
    secrets: tuple[str, ...],
) -> None:
    split = urlsplit(page.url)
    try:
        session = await fetch_browser_session(page)
    except Exception:
        session = {}
    try:
        cookies = await context.cookies([APP_URL])
    except Exception:
        cookies = []
    auth_cookie_present = any(
        "session" in str(cookie.get("name") or "").lower()
        or "auth" in str(cookie.get("name") or "").lower()
        for cookie in cookies
    )
    failure_code = (
        error.code if isinstance(error, LoginAuthenticationError) else type(error).__name__
    )
    diagnostic = {
        "captured_at": datetime.now(timezone.utc).isoformat(),
        "failure_code": failure_code,
        "error_type": type(error).__name__,
        "error_message": sanitize_text(error, secrets),
        "final_page": {
            "host": split.hostname or "",
            "path": split.path,
        },
        "session": {
            "status": session.get("status"),
            "verified": bool(session.get("verified")),
        },
        "cookies": {
            "leonardo_cookie_count": len(cookies),
            "auth_cookie_present": auth_cookie_present,
        },
        "auth_responses": auth_responses[-50:],
        "visible_messages": await collect_visible_messages(page, secrets),
        "form_outline": await collect_form_outline(page, secrets),
    }
    path = output_dir / "failure-diagnostic.json"
    path.write_text(json.dumps(diagnostic, indent=2), encoding="utf-8")
    path.chmod(0o600)


async def export_session(
    context: BrowserContext,
    page: Page,
    output_dir: Path,
    requests: list[dict[str, Any]],
) -> None:
    browser_session = await fetch_browser_session(page, force_refresh=True)
    if not browser_session.get("verified"):
        raise LoginAuthenticationError(
            "authenticated_session_missing",
            "exported cookies did not produce an authenticated session",
        )
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
    captured_auth_responses: list[dict[str, Any]] = []
    response_tasks: set[asyncio.Task[None]] = set()
    secrets = tuple(value for value in (args.email, args.password) if value)

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

        async def capture_response(response: Response) -> None:
            summary = await auth_response_summary(response, secrets)
            if summary:
                captured_auth_responses.append(summary)

        def schedule_response_capture(response: Response) -> None:
            task = asyncio.create_task(capture_response(response))
            response_tasks.add(task)
            task.add_done_callback(response_tasks.discard)

        page.on("request", capture)
        page.on("response", schedule_response_capture)
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

                if canva_sso_required(captured_auth_responses):
                    logging.info("Leonardo requires Canva SSO; continuing through Canva")
                    await complete_canva_sso(
                        page,
                        args.email,
                        args.password,
                        args.solve_captcha,
                    )
                    await wait_for_authenticated_login(
                        page,
                        args.timeout,
                        captured_auth_responses,
                    )
                else:
                    await wait_for_authenticated_login(
                        page,
                        args.timeout,
                        captured_auth_responses,
                    )
            await page.goto(APP_URL, wait_until="domcontentloaded")
            await page.wait_for_timeout(5000)
            await export_session(context, page, output_dir, captured_requests)
            print(f"Login succeeded. Sensitive session artifacts: {output_dir}")
        except Exception as exc:
            if response_tasks:
                await asyncio.gather(*tuple(response_tasks), return_exceptions=True)
            await page.screenshot(path=output_dir / "failure.png", full_page=True)
            await write_failure_diagnostic(
                context,
                page,
                output_dir,
                captured_auth_responses,
                exc,
                secrets,
            )
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
