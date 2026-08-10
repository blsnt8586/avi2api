#!/usr/bin/env python3
"""Capture Leonardo generation traffic from an authenticated browser profile."""

from __future__ import annotations

import argparse
import asyncio
import json
from pathlib import Path


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--profile-dir", type=Path, required=True)
    parser.add_argument("--output-dir", type=Path, required=True)
    parser.add_argument("--proxy")
    parser.add_argument("--channel", default="chrome")
    parser.add_argument("--headless", action="store_true")
    parser.add_argument("--prompt")
    parser.add_argument("--model", default="auto-preset")
    parser.add_argument("--wait", type=int, default=120)
    return parser.parse_args()


async def visible_controls(page) -> list[dict[str, str]]:
    return await page.locator("button, input, textarea, [role=button]").evaluate_all(
        """elements => elements
            .filter(element => {
                const style = getComputedStyle(element);
                const rect = element.getBoundingClientRect();
                return style.visibility !== 'hidden' && style.display !== 'none' &&
                    rect.width > 0 && rect.height > 0;
            })
            .map(element => ({
                tag: element.tagName.toLowerCase(),
                type: element.getAttribute('type') || '',
                text: (element.innerText || element.value || '').trim().slice(0, 200),
                placeholder: element.getAttribute('placeholder') || '',
                aria_label: element.getAttribute('aria-label') || '',
                data_testid: element.getAttribute('data-testid') || '',
            }))"""
    )


async def run(args: argparse.Namespace) -> None:
    from patchright.async_api import async_playwright

    output_dir = args.output_dir.resolve()
    output_dir.mkdir(parents=True, exist_ok=True)
    launch_options = {}
    if args.proxy:
        launch_options["proxy"] = {"server": args.proxy}

    async with async_playwright() as playwright:
        context = await playwright.chromium.launch_persistent_context(
            user_data_dir=args.profile_dir.resolve(),
            channel=args.channel,
            headless=args.headless,
            record_har_path=output_dir / "recording.har",
            record_har_content="embed",
            **launch_options,
        )
        page = await context.new_page()
        try:
            await page.goto(
                f"https://app.leonardo.ai/generate?model={args.model}",
                wait_until="domcontentloaded",
                timeout=120_000,
            )
            await page.wait_for_timeout(12_000)
            await page.screenshot(path=output_dir / "before.png", full_page=True)
            if args.prompt:
                prompt = page.locator('textarea[placeholder="Type a prompt..."]')
                await prompt.fill(args.prompt)
                generate = page.get_by_role("button", name="Generate", exact=True)
                await generate.wait_for(state="visible")
                if not await generate.is_enabled():
                    raise RuntimeError("Generate button remained disabled after entering the prompt")
                await generate.click()
                await page.screenshot(path=output_dir / "submitted.png", full_page=True)
                await page.wait_for_timeout(args.wait * 1000)
            await page.screenshot(path=output_dir / "page.png", full_page=True)
            controls = await visible_controls(page)
            (output_dir / "page.json").write_text(
                json.dumps({"url": page.url, "title": await page.title(), "controls": controls}, indent=2),
                encoding="utf-8",
            )
            print(json.dumps({"url": page.url, "controls": controls}, ensure_ascii=False))
        finally:
            await context.close()


def main() -> int:
    asyncio.run(run(parse_args()))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
