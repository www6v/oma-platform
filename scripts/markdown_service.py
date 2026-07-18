#!/usr/bin/env python3
"""Simple web page to Markdown conversion service.

This is a lightweight HTTP service that:
1. Fetches a web page
2. Converts HTML to clean Markdown
3. Returns the Markdown

Designed to work in China where direct access to some sites may be blocked.
Can be deployed on a server with good network connectivity.
"""

import asyncio
import os
import re
from typing import Optional

import httpx
from fastapi import FastAPI, HTTPException
from fastapi.responses import PlainTextResponse
from pydantic import BaseModel

app = FastAPI(title="HTML to Markdown Service")


class ConvertRequest(BaseModel):
    url: str
    max_length: Optional[int] = 50000


def html_to_markdown_simple(html: str) -> str:
    """Convert HTML to Markdown using basic regex rules.

    This is a simplified version that works without heavy dependencies.
    For production, use markdownify or similar.
    """
    # Remove script and style elements
    html = re.sub(r'<script[^>]*>.*?</script>', '', html, flags=re.DOTALL | re.IGNORECASE)
    html = re.sub(r'<style[^>]*>.*?</style>', '', html, flags=re.DOTALL | re.IGNORECASE)
    html = re.sub(r'<nav[^>]*>.*?</nav>', '', html, flags=re.DOTALL | re.IGNORECASE)
    html = re.sub(r'<footer[^>]*>.*?</footer>', '', html, flags=re.DOTALL | re.IGNORECASE)
    html = re.sub(r'<header[^>]*>.*?</header>', '', html, flags=re.DOTALL | re.IGNORECASE)

    # Extract title
    title_match = re.search(r'<title[^>]*>(.*?)</title>', html, re.IGNORECASE | re.DOTALL)
    title = title_match.group(1).strip() if title_match else ""

    # Extract main content (try common content areas)
    content = html
    for tag in ['article', 'main', '[role="main"]', '.content', '#content', '.post', '.article']:
        match = re.search(f'<{tag}[^>]*>(.*?)</{tag.split()[0]}>', html, re.DOTALL | re.IGNORECASE)
        if match:
            content = match.group(1)
            break

    # Convert headings
    for i in range(6, 0, -1):
        content = re.sub(
            rf'<h{i}[^>]*>(.*?)</h{i}>',
            lambda m, level=i: '\n' + '#' * level + ' ' + m.group(1).strip() + '\n',
            content,
            flags=re.IGNORECASE | re.DOTALL
        )

    # Convert paragraphs
    content = re.sub(r'<p[^>]*>(.*?)</p>', r'\n\1\n', content, flags=re.IGNORECASE | re.DOTALL)

    # Convert links
    content = re.sub(
        r'<a[^>]*href="([^"]*)"[^>]*>(.*?)</a>',
        r'[\2](\1)',
        content,
        flags=re.IGNORECASE | re.DOTALL
    )

    # Convert bold
    content = re.sub(r'<(?:strong|b)[^>]*>(.*?)</(?:strong|b)>', r'**\1**', content, flags=re.IGNORECASE | re.DOTALL)

    # Convert italic
    content = re.sub(r'<(?:em|i)[^>]*>(.*?)</(?:em|i)>', r'*\1*', content, flags=re.IGNORECASE | re.DOTALL)

    # Convert lists
    content = re.sub(r'<li[^>]*>(.*?)</li>', r'- \1', content, flags=re.IGNORECASE | re.DOTALL)

    # Remove remaining HTML tags
    content = re.sub(r'<[^>]+>', '', content)

    # Decode HTML entities
    content = content.replace('&nbsp;', ' ')
    content = content.replace('&amp;', '&')
    content = content.replace('&lt;', '<')
    content = content.replace('&gt;', '>')
    content = content.replace('&quot;', '"')
    content = content.replace('&#39;', "'")

    # Clean up whitespace
    content = re.sub(r'\n{3,}', '\n\n', content)
    content = re.sub(r' {2,}', ' ', content)
    content = '\n'.join(line.strip() for line in content.split('\n'))

    # Add title
    if title:
        content = f'# {title}\n\n{content}'

    return content.strip()


@app.get("/health")
async def health():
    return {"status": "ok"}


@app.post("/convert", response_class=PlainTextResponse)
async def convert(req: ConvertRequest):
    """Fetch URL and convert to Markdown."""
    try:
        # Fetch the page
        async with httpx.AsyncClient(
            follow_redirects=True,
            timeout=30.0,
            headers={
                "User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36"
            }
        ) as client:
            response = await client.get(req.url)
            response.raise_for_status()
            html = response.text

        # Convert to Markdown
        markdown = html_to_markdown_simple(html)

        # Truncate if needed
        if req.max_length and len(markdown) > req.max_length:
            markdown = markdown[:req.max_length] + "\n\n...[truncated]"

        return markdown

    except httpx.HTTPStatusError as e:
        raise HTTPException(status_code=e.response.status_code, detail=f"HTTP error: {e}")
    except httpx.RequestError as e:
        raise HTTPException(status_code=502, detail=f"Request error: {e}")
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Conversion error: {e}")


if __name__ == "__main__":
    import uvicorn
    port = int(os.environ.get("MARKDOWN_SERVICE_PORT", "8899"))
    uvicorn.run(app, host="0.0.0.0", port=port)
